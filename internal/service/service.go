// Package service ties the parts together: the bindings store, the key inventory, the link
// plans and the relay on the system's event stream. It is what the HTTP API calls.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/keys"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/links"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/relay"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/store"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/system"
)

// Options configure the service.
type Options struct {
	System   system.System
	Store    *store.Store
	Settings *store.Settings
	Log      func(string, ...any)
	// Lang is the language of the room names in the views (de by default).
	Lang string
	// CheckEvery is how often the owned links are compared with the plan; 10 min by default.
	CheckEvery time.Duration
	// PendingEvery is how often a pending binding is looked at; 1 min.
	PendingEvery time.Duration
	Now          func() time.Time
}

// States of a binding.
const (
	StateActive  = "active"       // links present, parameters as planned
	StatePending = "pending"      // the device has not taken the last write yet
	StateChanged = "changed"      // the links' parameters differ from the plan (changed elsewhere)
	StateMissing = "missing-link" // a link is gone
	StateError   = "error"        // the check could not read the system
	StateUnknown = "unknown"      // not checked yet
)

// Status is the result of the last check of a binding.
type Status struct {
	State     string                    `json:"state"`
	Detail    string                    `json:"detail,omitempty"`
	Differs   map[string]map[string]any `json:"differs,omitempty"`
	Missing   []string                  `json:"missing,omitempty"`
	CheckedAt time.Time                 `json:"checked_at,omitempty"`
}

// SenderView is a sender with its name.
type SenderView struct {
	model.Sender
	Name   string   `json:"name,omitempty"`
	Device string   `json:"device,omitempty"`
	Rooms  []string `json:"rooms,omitempty"`
}

// View is a binding as the UI shows it.
type View struct {
	model.Binding
	TargetName   string         `json:"target_name,omitempty"`
	TargetDevice string         `json:"target_device,omitempty"`
	TargetRooms  []string       `json:"target_rooms,omitempty"`
	Kind         profiles.Kind  `json:"kind"`
	UpName       string         `json:"up_name,omitempty"`
	DownName     string         `json:"down_name,omitempty"`
	SenderViews  []SenderView   `json:"sender_views"`
	Status       Status         `json:"status"`
	Activity     relay.Activity `json:"activity"`
}

// Event is what the service broadcasts to the UI's stream.
type Event struct {
	Type string `json:"type"` // press, level, binding, check
	// press and level
	Interface string  `json:"interface,omitempty"`
	Address   string  `json:"address,omitempty"`
	Key       string  `json:"key,omitempty"`
	Name      string  `json:"name,omitempty"`
	Level     float64 `json:"level,omitempty"`
	// binding and check
	ID    string    `json:"id,omitempty"`
	State string    `json:"state,omitempty"`
	TS    time.Time `json:"ts"`
}

// Service is the addon's core.
type Service struct {
	o     Options
	relay *relay.Relay
	mu    sync.Mutex
	// status per binding id
	status map[string]Status
	// the caches
	objects   system.Objects
	objectsAt time.Time
	devices   map[string][]litesource.DeviceDescription
	ifaces    []litesource.Interface
	ifacesAt  time.Time
	subs      map[chan Event]struct{}
	checkNow  chan struct{}
	streamOK  bool
	streamErr string
	started   time.Time
	presses   int64
}

// New makes the service; Run starts it.
func New(o Options) *Service {
	if o.Log == nil {
		o.Log = log.Printf
	}
	if o.Lang == "" {
		o.Lang = "de"
	}
	if o.CheckEvery <= 0 {
		o.CheckEvery = 10 * time.Minute
	}
	if o.PendingEvery <= 0 {
		o.PendingEvery = time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	s := &Service{o: o, status: map[string]Status{}, devices: map[string][]litesource.DeviceDescription{}, subs: map[chan Event]struct{}{}, checkNow: make(chan struct{}, 1)}
	st := o.Settings.Get()
	s.relay = relay.New(relay.Options{
		Emit: relay.EmitFunc(func(ctx context.Context, iface, key, datapoint string) error {
			return system.Press(ctx, o.System, iface, key, datapoint)
		}),
		Interval:    time.Duration(st.StepIntervalMS) * time.Millisecond,
		HoldTimeout: time.Duration(st.HoldTimeoutMS) * time.Millisecond,
		Log:         o.Log,
		LogPresses:  st.LogPresses,
		Now:         o.Now,
	})
	s.reload()
	return s
}

// reload hands the store's bindings to the relay.
func (s *Service) reload() {
	st := s.o.Settings.Get()
	s.relay.SetBindings(s.o.Store.List(), time.Duration(st.StepIntervalMS)*time.Millisecond)
}

// Run follows the system's event stream and checks the links until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	s.started = s.o.Now()
	s.mu.Unlock()
	go s.checker(ctx)
	s.requestCheck()
	for ctx.Err() == nil {
		s.stream(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *Service) stream(ctx context.Context) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := s.o.System.Subscribe(sctx, litesource.Filter{}, "")
	for m := range ch {
		switch m.Type {
		case "hello":
			s.setStream(true, "")
		case "event", "state":
			if m.Event == nil {
				continue
			}
			s.handleEvent(ctx, *m.Event, m.Type == "event")
		case "newDevices", "deleteDevices", "replaceDevice", "readdedDevice", "updateDevice":
			s.mu.Lock()
			if m.Devices != nil {
				delete(s.devices, m.Devices.Interface)
			}
			s.mu.Unlock()
			if m.Type != "updateDevice" || (m.Devices != nil && m.Devices.Hint == 1) {
				s.requestCheck()
			}
		case "interface":
			if m.Interface != nil && (m.Interface.State == "up" || m.Interface.State == "restarted") {
				s.requestCheck()
			}
		case "resync":
			// presses are never replayed; the levels come with the next reports
		}
	}
	s.setStream(false, "the event stream ended")
}

func (s *Service) setStream(ok bool, detail string) {
	s.mu.Lock()
	s.streamOK, s.streamErr = ok, detail
	s.mu.Unlock()
}

func (s *Service) handleEvent(ctx context.Context, e litesource.Event, live bool) {
	switch {
	case relay.IsPress(e.Key):
		if !live {
			return
		}
		s.mu.Lock()
		s.presses++
		s.mu.Unlock()
		s.relay.Handle(ctx, relay.Press{Interface: e.Interface, Address: e.Address, Key: e.Key, TS: e.TS})
		s.broadcast(Event{Type: "press", Interface: e.Interface, Address: e.Address, Key: e.Key, TS: s.o.Now()})
	case e.Key == "LEVEL":
		if v, ok := e.Float(); ok {
			s.relay.Level(e.Interface, e.Address, v)
			s.broadcast(Event{Type: "level", Interface: e.Interface, Address: e.Address, Level: v, TS: s.o.Now()})
		}
	}
}

// --- the checks -----------------------------------------------------------------------------

func (s *Service) requestCheck() {
	select {
	case s.checkNow <- struct{}{}:
	default:
	}
}

func (s *Service) checker(ctx context.Context) {
	t := time.NewTicker(s.o.PendingEvery)
	defer t.Stop()
	last := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.checkNow:
			s.Check(ctx)
			last = s.o.Now()
		case <-t.C:
			if s.anyPending() || s.o.Now().Sub(last) >= s.o.CheckEvery {
				s.Check(ctx)
				last = s.o.Now()
			}
		}
	}
}

func (s *Service) anyPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.status {
		if st.State == StatePending || st.State == StateUnknown || st.State == StateError {
			return true
		}
	}
	for _, b := range s.o.Store.List() {
		for _, snd := range b.Senders {
			if NeedsUsage(snd) && (snd.Usage == nil || snd.Usage.State != model.UsageReported) {
				return true
			}
		}
	}
	return false
}

// Check compares every binding's links with its plan and records the status.
func (s *Service) Check(ctx context.Context) {
	bs := s.o.Store.List()
	linksOf := map[string][]system.Link{}
	errOf := map[string]error{}
	for _, b := range bs {
		if _, seen := linksOf[b.Target.Interface]; seen {
			continue
		}
		if _, failed := errOf[b.Target.Interface]; failed {
			continue
		}
		l, err := system.Links(ctx, s.o.System, b.Target.Interface)
		if err != nil {
			errOf[b.Target.Interface] = err
			continue
		}
		linksOf[b.Target.Interface] = l
	}
	for _, b := range bs {
		st := Status{CheckedAt: s.o.Now()}
		if err := errOf[b.Target.Interface]; err != nil {
			st.State, st.Detail = StateError, err.Error()
			s.setStatus(b.ID, st)
			continue
		}
		s.setStatus(b.ID, s.checkOne(ctx, b, linksOf[b.Target.Interface], st))
		s.reportUsages(ctx, b.ID)
	}
	s.broadcast(Event{Type: "check", TS: s.o.Now()})
}

func (s *Service) checkOne(ctx context.Context, b model.Binding, ls []system.Link, st Status) Status {
	differs, missing, err := links.Verify(ctx, s.o.System, b, ls)
	if err != nil {
		st.State, st.Detail = StateError, err.Error()
		return st
	}
	if len(missing) > 0 {
		st.State, st.Missing = StateMissing, missing
		st.Detail = "link missing: " + strings.Join(missing, ", ")
		return st
	}
	if b.Adopted {
		// an adopted link keeps its own parameters: only the direction must still fit
		differs = s.adoptedDrift(ctx, b)
	}
	if len(differs) > 0 {
		pending, _ := system.ConfigPending(ctx, s.o.System, b.Target.Interface, system.DeviceOf(b.Target.Address))
		if pending {
			st.State, st.Detail = StatePending, "the device has not taken the parameters yet"
		} else {
			st.State, st.Detail = StateChanged, "the link parameters differ from the profile"
		}
		st.Differs = differs
		return st
	}
	if pending, _ := system.ConfigPending(ctx, s.o.System, b.Target.Interface, system.DeviceOf(b.Target.Address)); pending {
		st.State, st.Detail = StatePending, "the device has a configuration pending"
		return st
	}
	st.State = StateActive
	return st
}

// adoptedDrift answers what differs on an adopted binding: a key whose link no longer dims the
// way the binding says.
func (s *Service) adoptedDrift(ctx context.Context, b model.Binding) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, dir := range []string{"up", "down"} {
		key := b.Key(dir)
		ps, err := system.LinkParamset(ctx, s.o.System, b.Target.Interface, b.Target.Address, key)
		if err != nil {
			continue
		}
		c := profiles.Classify(b.Target.Family, ps)
		if c.Long != dir {
			out[key] = map[string]any{"long": c.Long, "short": c.Short}
		}
	}
	return out
}

func (s *Service) setStatus(id string, st Status) {
	s.mu.Lock()
	s.status[id] = st
	s.mu.Unlock()
}

// --- caches --------------------------------------------------------------------------------

func (s *Service) objectsCached(ctx context.Context) (system.Objects, error) {
	s.mu.Lock()
	if s.objects != nil && s.o.Now().Sub(s.objectsAt) < 20*time.Second {
		o := s.objects
		s.mu.Unlock()
		return o, nil
	}
	s.mu.Unlock()
	o, err := s.o.System.Objects(ctx, s.o.Lang)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.objects, s.objectsAt = o, s.o.Now()
	s.mu.Unlock()
	return o, nil
}

func (s *Service) forgetObjects() {
	s.mu.Lock()
	s.objects = nil
	s.mu.Unlock()
}

func (s *Service) interfaces(ctx context.Context) ([]litesource.Interface, error) {
	s.mu.Lock()
	if s.ifaces != nil && s.o.Now().Sub(s.ifacesAt) < time.Minute {
		i := s.ifaces
		s.mu.Unlock()
		return i, nil
	}
	s.mu.Unlock()
	all, err := s.o.System.Interfaces(ctx)
	if err != nil {
		return nil, err
	}
	out := []litesource.Interface{}
	for _, i := range all {
		if profiles.KeyDevice(i.Name) != "" {
			out = append(out, i)
		}
	}
	s.mu.Lock()
	s.ifaces, s.ifacesAt = out, s.o.Now()
	s.mu.Unlock()
	return out, nil
}

func (s *Service) devicesOf(ctx context.Context, iface string) ([]litesource.DeviceDescription, error) {
	s.mu.Lock()
	d, ok := s.devices[iface]
	s.mu.Unlock()
	if ok {
		return d, nil
	}
	d, err := s.o.System.Devices(ctx, iface)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.devices[iface] = d
	s.mu.Unlock()
	return d, nil
}

func (s *Service) channelType(ctx context.Context, iface, address string) (channelType, deviceType string, err error) {
	devs, err := s.devicesOf(ctx, iface)
	if err != nil {
		return "", "", err
	}
	parent := ""
	for _, d := range devs {
		if d.Address == address {
			channelType, parent = d.Type, d.Parent
		}
	}
	if channelType == "" {
		return "", "", fmt.Errorf("%s has no channel %s", iface, address)
	}
	for _, d := range devs {
		if d.Address == parent {
			deviceType = d.Type
		}
	}
	return channelType, deviceType, nil
}

// --- reads ---------------------------------------------------------------------------------

// Health is the service's state for the health route.
type Health struct {
	Stream   bool   `json:"stream"`
	Detail   string `json:"stream_detail,omitempty"`
	Bindings int    `json:"bindings"`
	Presses  int64  `json:"presses"`
	Pending  int    `json:"pending"`
	Changed  int    `json:"changed"`
}

// Health answers the state.
func (s *Service) Health() Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := Health{Stream: s.streamOK, Detail: s.streamErr, Bindings: len(s.o.Store.List()), Presses: s.presses}
	for _, st := range s.status {
		switch st.State {
		case StatePending:
			h.Pending++
		case StateChanged, StateMissing:
			h.Changed++
		}
	}
	return h
}

// Interfaces lists the interfaces with a virtual remote.
func (s *Service) Interfaces(ctx context.Context) ([]litesource.Interface, error) {
	return s.interfaces(ctx)
}

// Bindings answers every binding as a view.
func (s *Service) Bindings(ctx context.Context) ([]View, error) {
	objects, err := s.objectsCached(ctx)
	if err != nil {
		objects = system.Objects{}
	}
	var out []View
	for _, b := range s.o.Store.List() {
		out = append(out, s.view(ctx, b, objects))
	}
	if out == nil {
		out = []View{}
	}
	return out, err
}

// Binding answers one view.
func (s *Service) Binding(ctx context.Context, id string) (View, bool) {
	b, ok := s.o.Store.Get(id)
	if !ok {
		return View{}, false
	}
	objects, _ := s.objectsCached(ctx)
	return s.view(ctx, b, objects), true
}

func (s *Service) view(ctx context.Context, b model.Binding, objects system.Objects) View {
	v := View{Binding: b, Kind: b.Target.Family.Kind(), SenderViews: []SenderView{}}
	t := objects.Get(b.Target.Interface, b.Target.Address)
	v.TargetName, v.TargetRooms = t.Name, t.Rooms
	_, v.TargetDevice, _ = s.channelType(ctx, b.Target.Interface, b.Target.Address)
	v.UpName = objects.Get(b.Target.Interface, b.Up).Name
	v.DownName = objects.Get(b.Target.Interface, b.Down).Name
	for _, snd := range b.Senders {
		o := objects.Get(snd.Interface, snd.Address)
		_, dev, _ := s.channelType(ctx, snd.Interface, snd.Address)
		v.SenderViews = append(v.SenderViews, SenderView{Sender: snd, Name: o.Name, Device: dev, Rooms: o.Rooms})
	}
	s.mu.Lock()
	st, ok := s.status[b.ID]
	s.mu.Unlock()
	if !ok {
		st = Status{State: StateUnknown}
	}
	v.Status = st
	v.Activity, _ = s.relay.Activity(b.ID)
	return v
}

// Targets lists the dimmer and blind channels of every interface.
func (s *Service) Targets(ctx context.Context) ([]keys.Channel, error) {
	ifaces, err := s.interfaces(ctx)
	if err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	out := []keys.Channel{}
	for _, i := range ifaces {
		devs, err := s.devicesOf(ctx, i.Name)
		if err != nil {
			s.o.Log("longpressdim: %s: %v", i.Name, err)
			continue
		}
		out = append(out, keys.Targets(i.Name, devs, objects)...)
	}
	return out, nil
}

// Senders lists the key channels of every interface.
func (s *Service) Senders(ctx context.Context) ([]keys.Channel, error) {
	ifaces, err := s.interfaces(ctx)
	if err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	out := []keys.Channel{}
	for _, i := range ifaces {
		devs, err := s.devicesOf(ctx, i.Name)
		if err != nil {
			continue
		}
		cs, err := keys.Senders(ctx, s.o.System, i.Name, devs, objects)
		if err != nil {
			s.o.Log("longpressdim: senders of %s: %v", i.Name, err)
			continue
		}
		out = append(out, cs...)
	}
	return out, nil
}

// Keys answers the inventory of an interface's virtual keys.
func (s *Service) Keys(ctx context.Context, iface string) ([]keys.Key, error) {
	if profiles.KeyDevice(iface) == "" {
		return nil, fmt.Errorf("%s has no virtual remote", iface)
	}
	ls, err := system.Links(ctx, s.o.System, iface)
	if err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	return keys.Inventory(iface, ls, objects), nil
}

// Foreign lists the links from virtual keys to targets that no binding owns.
func (s *Service) Foreign(ctx context.Context) ([]keys.ForeignLink, error) {
	ifaces, err := s.interfaces(ctx)
	if err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	for _, b := range s.o.Store.List() {
		owned[b.Target.Interface+"|"+b.Up] = true
		owned[b.Target.Interface+"|"+b.Down] = true
	}
	out := []keys.ForeignLink{}
	for _, i := range ifaces {
		ls, err := system.Links(ctx, s.o.System, i.Name)
		if err != nil {
			s.o.Log("longpressdim: links of %s: %v", i.Name, err)
			continue
		}
		devs, err := s.devicesOf(ctx, i.Name)
		if err != nil {
			continue
		}
		out = append(out, keys.Foreign(ctx, s.o.System, i.Name, ls, keys.Targets(i.Name, devs, objects), objects, owned)...)
	}
	return out, nil
}

// --- writes --------------------------------------------------------------------------------

var idClean = regexp.MustCompile(`[^a-z0-9]+`)

// complete fills what a draft leaves open: the id, the target's type and family, the keys.
func (s *Service) complete(ctx context.Context, b model.Binding) (model.Binding, error) {
	ct, _, err := s.channelType(ctx, b.Target.Interface, b.Target.Address)
	if err != nil {
		return b, err
	}
	b.Target.Type = ct
	f, ok := profiles.FamilyOf(b.Target.Interface, ct)
	if !ok {
		return b, fmt.Errorf("%s %s is a %s, not a dimmer or blind the addon links to", b.Target.Interface, b.Target.Address, ct)
	}
	b.Target.Family = f
	if b.Mode == "" {
		b.Mode = model.TwoKey
	}
	if b.ID == "" {
		b.ID = strings.Trim(idClean.ReplaceAllString(strings.ToLower(b.Target.Address), "-"), "-")
		if _, taken := s.o.Store.Get(b.ID); taken {
			for n := 2; ; n++ {
				if _, taken := s.o.Store.Get(fmt.Sprintf("%s-%d", b.ID, n)); !taken {
					b.ID = fmt.Sprintf("%s-%d", b.ID, n)
					break
				}
			}
		}
	}
	if b.Up == "" || b.Down == "" {
		inv, err := s.Keys(ctx, b.Target.Interface)
		if err != nil {
			return b, err
		}
		// a key the binding already holds stays free for it
		if old, ok := s.o.Store.Get(b.ID); ok {
			for i := range inv {
				if inv[i].Address == old.Up || inv[i].Address == old.Down {
					inv[i].Free = inv[i].Marker != nil && inv[i].Marker.Binding == b.ID
				}
			}
		}
		need := 0
		if b.Up == "" {
			need++
		}
		if b.Down == "" {
			need++
		}
		picked, err := keys.Pick(inv, need)
		if err != nil {
			return b, err
		}
		if b.Up == "" {
			b.Up, picked = picked[0], picked[1:]
		}
		if b.Down == "" {
			b.Down = picked[0]
		}
	}
	if b.Senders == nil {
		b.Senders = []model.Sender{}
	}
	return b, b.Validate()
}

// checkKeys refuses a key another binding owns or that is not free (unless the binding holds it
// already or adopts it).
func (s *Service) checkKeys(ctx context.Context, b model.Binding, adopting bool) error {
	inv, err := s.Keys(ctx, b.Target.Interface)
	if err != nil {
		return err
	}
	for _, key := range []string{b.Up, b.Down} {
		if id, _ := s.o.Store.KeyOwner(b.Target.Interface, key); id != "" && id != b.ID {
			return fmt.Errorf("%s belongs to the binding %s", key, id)
		}
		k, ok := keys.Find(inv, key)
		if !ok {
			return fmt.Errorf("%s is not a virtual key", key)
		}
		if k.Marker != nil && k.Marker.Binding != b.ID {
			return fmt.Errorf("%s is marked for the binding %s", key, k.Marker.Binding)
		}
		if adopting {
			continue
		}
		own := k.Marker != nil && k.Marker.Binding == b.ID
		if own {
			continue
		}
		if k.Foreign {
			return fmt.Errorf("%s carries another consumer's metadata", key)
		}
		for _, l := range k.Links {
			if l.Sender != key || l.Receiver != b.Target.Address {
				return fmt.Errorf("%s is linked already (%s -> %s)", key, l.Sender, l.Receiver)
			}
		}
		if !keys.Unnamed(key, k.Name) && len(k.Links) == 0 {
			return fmt.Errorf("%s is named %q: a key with a name is in use somewhere", key, k.Name)
		}
	}
	return nil
}

// Plan computes a binding's plan without writing (the dry run).
func (s *Service) Plan(ctx context.Context, b model.Binding) (*links.Plan, error) {
	s.forgetObjects() // the names and markers must be fresh for the key checks
	b, err := s.complete(ctx, b)
	if err != nil {
		return nil, err
	}
	if err := s.checkKeys(ctx, b, b.Adopted); err != nil {
		return nil, err
	}
	ls, err := system.Links(ctx, s.o.System, b.Target.Interface)
	if err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	if old, ok := s.o.Store.Get(b.ID); ok {
		b.Created = old.Created
		if old.Adopted {
			b.Adopted = true
		}
		// keys that change: the old ones are released afterwards
		if old.Up != b.Up || old.Down != b.Down {
			return nil, errors.New("a binding's keys cannot be changed; remove it and create it anew")
		}
	}
	return links.PlanApply(ctx, s.o.System, b, ls, objects)
}

// Apply plans and writes a binding, then stores it.
func (s *Service) Apply(ctx context.Context, b model.Binding) (*links.Result, error) {
	p, err := s.Plan(ctx, b)
	if err != nil {
		return nil, err
	}
	r := links.Apply(ctx, s.o.System, p)
	s.forgetObjects()
	if r.Failed != "" && !linksExist(r) {
		return r, fmt.Errorf("%s", r.Failed)
	}
	// the links are the binding's now: it is stored even after a later failure, so the user
	// can retry (reapply) or remove it
	var gone []model.Sender
	if old, ok := s.o.Store.Get(p.Binding.ID); ok {
		for i, snd := range p.Binding.Senders {
			for _, o := range old.Senders {
				if o.Same(snd) {
					p.Binding.Senders[i].Usage = o.Usage
				}
			}
		}
		for _, o := range old.Senders {
			kept := false
			for _, snd := range p.Binding.Senders {
				kept = kept || snd.Same(o)
			}
			if !kept {
				gone = append(gone, o)
			}
		}
	}
	if err := s.o.Store.Put(p.Binding); err != nil {
		return r, err
	}
	s.releaseUsages(ctx, gone, p.Binding.ID)
	s.reportUsages(ctx, p.Binding.ID)
	s.afterWrite(ctx, p.Binding.ID, r)
	if r.Failed != "" {
		return r, fmt.Errorf("%s", r.Failed)
	}
	return r, nil
}

// linksExist tells whether a failed apply still created links (then the binding is stored so
// that the user can retry or remove it; the links are its).
func linksExist(r *links.Result) bool {
	for _, sr := range r.Steps {
		if sr.Step.Kind == links.AddLink && (sr.State == "done" || sr.State == "pending") {
			return true
		}
	}
	return false
}

func (s *Service) afterWrite(ctx context.Context, id string, r *links.Result) {
	s.reload()
	st := Status{CheckedAt: s.o.Now(), State: StateActive}
	switch {
	case r.Failed != "":
		st.State, st.Detail = StateError, r.Failed
	case r.Pending:
		st.State, st.Detail = StatePending, "waiting for the device"
	case len(r.Differs) > 0:
		st.State, st.Detail, st.Differs = StateChanged, "read back differently", r.Differs
	}
	s.setStatus(id, st)
	s.broadcast(Event{Type: "binding", ID: id, State: st.State, TS: s.o.Now()})
}

// Reapply writes a binding's profile again (after a change elsewhere).
func (s *Service) Reapply(ctx context.Context, id string) (*links.Result, error) {
	b, ok := s.o.Store.Get(id)
	if !ok {
		return nil, ErrNotFound
	}
	b.Adopted = false // from now on the addon's profile is the reference
	return s.Apply(ctx, b)
}

// ErrNotFound: no such binding.
var ErrNotFound = errors.New("no such binding")

// PlanRemove computes a binding's removal.
func (s *Service) PlanRemove(ctx context.Context, id string, forget bool) (*links.Plan, error) {
	b, ok := s.o.Store.Get(id)
	if !ok {
		return nil, ErrNotFound
	}
	ls, err := system.Links(ctx, s.o.System, b.Target.Interface)
	if err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	return links.PlanRemove(b, ls, objects, forget), nil
}

// Remove removes a binding: its links (unless forget), the keys' markers, the store entry.
func (s *Service) Remove(ctx context.Context, id string, forget bool) (*links.Result, error) {
	p, err := s.PlanRemove(ctx, id, forget)
	if err != nil {
		return nil, err
	}
	r := links.Apply(ctx, s.o.System, p)
	s.forgetObjects()
	if r.Failed != "" {
		return r, fmt.Errorf("%s", r.Failed)
	}
	if err := s.o.Store.Delete(id); err != nil {
		return r, err
	}
	s.releaseUsages(ctx, p.Binding.Senders, id)
	s.mu.Lock()
	delete(s.status, id)
	s.mu.Unlock()
	s.reload()
	s.broadcast(Event{Type: "binding", ID: id, State: "removed", TS: s.o.Now()})
	return r, nil
}

// RemoveAll removes every binding (the settings page's button); it goes on after a failure
// and answers the first error.
func (s *Service) RemoveAll(ctx context.Context, forget bool) (removed int, err error) {
	for _, b := range s.o.Store.List() {
		if _, e := s.Remove(ctx, b.ID, forget); e != nil {
			if err == nil {
				err = fmt.Errorf("%s: %w", b.ID, e)
			}
			continue
		}
		removed++
	}
	return removed, err
}

// Adopt takes existing links from two virtual keys to a target as a binding: the keys are
// marked, the links are left as they are, the mode and step come from their paramsets.
func (s *Service) Adopt(ctx context.Context, iface, target, up, down string, senders []model.Sender) (*links.Result, error) {
	ct, _, err := s.channelType(ctx, iface, target)
	if err != nil {
		return nil, err
	}
	f, ok := profiles.FamilyOf(iface, ct)
	if !ok {
		return nil, fmt.Errorf("%s is a %s", target, ct)
	}
	ls, err := system.Links(ctx, s.o.System, iface)
	if err != nil {
		return nil, err
	}
	b := model.Binding{Target: model.Target{Interface: iface, Address: target, Type: ct, Family: f}, Mode: model.TwoKey, Up: up, Down: down, Senders: senders, Adopted: true}
	for _, dir := range []string{"up", "down"} {
		key := b.Key(dir)
		found := false
		for _, l := range ls {
			if l.Sender == key && l.Receiver == target {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no link %s -> %s to adopt", key, target)
		}
		ps, err := system.LinkParamset(ctx, s.o.System, iface, target, key)
		if err != nil {
			return nil, err
		}
		c := profiles.Classify(f, ps)
		if c.Long != dir {
			return nil, fmt.Errorf("%s does not dim %s (long press: %s)", key, dir, c.Long)
		}
		if dir == "up" {
			if c.Short == "toggle" {
				b.Mode = model.OneKey
			}
			if c.OnLevel > 0 {
				b.Options.OnLevel = c.OnLevel
			}
		}
		if c.Step > 0 {
			if f.Kind() == profiles.Dimmer {
				b.Options.Step = c.Step
			} else {
				b.Options.Travel = c.Step
			}
		}
	}
	b, err = s.complete(ctx, b)
	if err != nil {
		return nil, err
	}
	if err := s.checkKeys(ctx, b, true); err != nil {
		return nil, err
	}
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	// marking only: the plan's metadata steps, no link writes
	full, err := links.PlanApply(ctx, s.o.System, b, ls, objects)
	if err != nil {
		return nil, err
	}
	p := &links.Plan{Binding: b, Warnings: full.Warnings}
	for _, st := range full.Steps {
		if st.Kind == links.SetMeta {
			p.Steps = append(p.Steps, st)
		}
	}
	r := links.Apply(ctx, s.o.System, p)
	s.forgetObjects()
	if r.Failed != "" {
		return r, fmt.Errorf("%s", r.Failed)
	}
	if err := s.o.Store.Put(b); err != nil {
		return r, err
	}
	s.reportUsages(ctx, b.ID)
	s.afterWrite(ctx, b.ID, r)
	return r, nil
}

// SetSenders replaces a binding's senders and options that need no link write.
func (s *Service) SetSenders(ctx context.Context, id string, senders []model.Sender, stepIntervalMS int) (View, error) {
	b, ok := s.o.Store.Get(id)
	if !ok {
		return View{}, ErrNotFound
	}
	old := b.Senders
	b.Senders = []model.Sender{}
	for _, snd := range senders {
		snd.Usage = nil
		for _, o := range old {
			if o.Same(snd) {
				snd.Usage = o.Usage
			}
		}
		b.Senders = append(b.Senders, snd)
	}
	b.StepIntervalMS = stepIntervalMS
	if err := b.Validate(); err != nil {
		return View{}, err
	}
	if err := s.o.Store.Put(b); err != nil {
		return View{}, err
	}
	var gone []model.Sender
	for _, o := range old {
		kept := false
		for _, snd := range b.Senders {
			kept = kept || snd.Same(o)
		}
		if !kept {
			gone = append(gone, o)
		}
	}
	s.releaseUsages(ctx, gone, "")
	s.reportUsages(ctx, id)
	s.reload()
	s.broadcast(Event{Type: "binding", ID: id, State: "updated", TS: s.o.Now()})
	v, _ := s.Binding(ctx, id)
	return v, nil
}

// Settings answers the settings.
func (s *Service) Settings() model.Settings { return s.o.Settings.Get() }

// SetSettings stores the settings and hands them to the relay.
func (s *Service) SetSettings(v model.Settings) (model.Settings, error) {
	if err := s.o.Settings.Set(v); err != nil {
		return model.Settings{}, err
	}
	st := s.o.Settings.Get()
	s.relay.Configure(time.Duration(st.StepIntervalMS)*time.Millisecond, time.Duration(st.HoldTimeoutMS)*time.Millisecond, st.LogPresses)
	s.reload()
	return st, nil
}

// TestPress presses a binding's key (the UI's test buttons).
func (s *Service) TestPress(ctx context.Context, id, direction, datapoint string) error {
	b, ok := s.o.Store.Get(id)
	if !ok {
		return ErrNotFound
	}
	if datapoint != "PRESS_SHORT" && datapoint != "PRESS_LONG" {
		return fmt.Errorf("invalid datapoint %q", datapoint)
	}
	return system.Press(ctx, s.o.System, b.Target.Interface, b.Key(direction), datapoint)
}

// --- value usages --------------------------------------------------------------------------

// UsageValues are the datapoints a sender's usage is registered for.
var UsageValues = []string{"PRESS_SHORT", "PRESS_LONG"}

// NeedsUsage tells whether a sender's interface process reports a key's presses only once a
// usage is registered: hmipserver does (HmIP and HmIPW keys); rfd's BidCos keys send to the
// central without it.
func NeedsUsage(snd model.Sender) bool { return snd.Interface == "HmIP-RF" }

// report calls reportValueUsage for every datapoint of a sender with the count and answers
// the outcome: reported, pending (the process answered false: the device must be reached
// first, CONFIG_PENDING) or failed.
func (s *Service) report(ctx context.Context, snd model.Sender, count int) *model.Usage {
	u := &model.Usage{State: model.UsageReported, At: s.o.Now()}
	if count == 0 {
		u.State = model.UsageReleased
	}
	for _, v := range UsageValues {
		raw, err := s.o.System.Call(ctx, snd.Interface, "reportValueUsage", snd.Address, v, count)
		if err != nil {
			u.State, u.Detail = model.UsageFailed, err.Error()
			s.o.Log("longpressdim: reportValueUsage %s %s %d: %v", snd.Address, v, count, err)
			return u
		}
		var ok bool
		if json.Unmarshal(raw, &ok) == nil && !ok && u.State != model.UsageFailed {
			u.State, u.Detail = model.UsagePending, "the device has not taken the usage yet"
		}
	}
	return u
}

// reportUsages registers the usages of a binding's HmIP senders that are not reported yet
// (or whose last report failed or is pending) and writes the outcome into the store.
func (s *Service) reportUsages(ctx context.Context, id string) {
	b, ok := s.o.Store.Get(id)
	if !ok {
		return
	}
	changed := false
	for i, snd := range b.Senders {
		if !NeedsUsage(snd) {
			continue
		}
		if snd.Usage != nil && snd.Usage.State == model.UsageReported {
			continue
		}
		b.Senders[i].Usage = s.report(ctx, snd, 1)
		changed = true
	}
	if changed {
		if err := s.o.Store.Put(b); err != nil {
			s.o.Log("longpressdim: %v", err)
		}
	}
}

// releaseUsages sets the usage of HmIP senders back to 0 - the ones no other binding uses,
// unless the settings keep them.
func (s *Service) releaseUsages(ctx context.Context, senders []model.Sender, exceptID string) {
	if s.o.Settings.Get().KeepUsages {
		return
	}
	for _, snd := range senders {
		if !NeedsUsage(snd) || s.usedElsewhere(snd, exceptID) {
			continue
		}
		s.report(ctx, snd, 0)
	}
}

func (s *Service) usedElsewhere(snd model.Sender, exceptID string) bool {
	for _, b := range s.o.Store.List() {
		if b.ID == exceptID {
			continue
		}
		for _, o := range b.Senders {
			if o.Same(snd) {
				return true
			}
		}
	}
	return false
}

// --- the flow import -----------------------------------------------------------------------

// MappingEntry is one line of a pasted Node-RED mapping, resolved.
type MappingEntry struct {
	SenderName string `json:"sender_name"`
	KeyName    string `json:"key_name"`
	// Sender is the resolved key channel; empty when the name was not found.
	Sender *model.Sender `json:"sender,omitempty"`
	// Key is the resolved virtual key and its interface.
	KeyInterface string `json:"key_interface,omitempty"`
	Key          string `json:"key,omitempty"`
	// Binding is the binding that owns the key, "" when none (adopt first).
	Binding string `json:"binding,omitempty"`
	Role    string `json:"role,omitempty"`
	Error   string `json:"error,omitempty"`
}

var mappingLine = regexp.MustCompile(`['"]([^'"]+)['"]\s*:\s*['"]([^'"]+)['"]`)

// ParseMapping reads the `'<sender channel name>': '<key name>'` pairs of a pasted mapping
// (the JavaScript object literal of the Node-RED flow) and resolves both names through the
// system's names: a sender name is a channel's name, or "<device name>:<n>"; a key name is a
// virtual key's name.
func (s *Service) ParseMapping(ctx context.Context, text string) ([]MappingEntry, error) {
	objects, err := s.objectsCached(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string][]string{} // name -> refs
	for ref, o := range objects {
		if o.Name != "" {
			byName[o.Name] = append(byName[o.Name], ref)
		}
	}
	resolve := func(name string) (iface, address string, err error) {
		refs := byName[name]
		if len(refs) == 0 {
			if i := strings.LastIndexByte(name, ':'); i > 0 {
				var n int
				if _, e := fmt.Sscanf(name[i+1:], "%d", &n); e == nil {
					for _, dref := range byName[name[:i]] {
						j := strings.IndexByte(dref, '.')
						if j > 0 && !strings.Contains(dref[j+1:], ":") {
							refs = append(refs, fmt.Sprintf("%s:%d", dref, n))
						}
					}
				}
			}
		}
		if len(refs) == 0 {
			return "", "", fmt.Errorf("no channel named %q", name)
		}
		if len(refs) > 1 {
			sort.Strings(refs)
			return "", "", fmt.Errorf("%q names %d channels (%s)", name, len(refs), strings.Join(refs, ", "))
		}
		j := strings.IndexByte(refs[0], '.')
		return refs[0][:j], refs[0][j+1:], nil
	}
	var out []MappingEntry
	for _, m := range mappingLine.FindAllStringSubmatch(text, -1) {
		e := MappingEntry{SenderName: m[1], KeyName: m[2]}
		if iface, addr, err := resolve(e.SenderName); err != nil {
			e.Error = err.Error()
		} else {
			e.Sender = &model.Sender{Interface: iface, Address: addr}
		}
		if iface, addr, err := resolve(e.KeyName); err != nil {
			e.Error = strings.TrimSpace(e.Error + "; " + err.Error())
			e.Error = strings.TrimPrefix(e.Error, "; ")
		} else if !strings.HasPrefix(addr, profiles.KeyDevice(iface)+":") {
			e.Error = strings.TrimPrefix(strings.TrimSpace(e.Error+"; "+fmt.Sprintf("%q is not a virtual key", e.KeyName)), "; ")
		} else {
			e.KeyInterface, e.Key = iface, addr
			if id, role := s.o.Store.KeyOwner(iface, addr); id != "" {
				e.Binding, e.Role = id, role
				if e.Sender != nil {
					e.Sender.Role = role
					if e.Sender.Interface == iface {
						e.Error = "the sender is on the key's own interface: link it directly"
					}
				}
			} else {
				e.Error = strings.TrimPrefix(strings.TrimSpace(e.Error+"; no binding owns this key yet (adopt its links first)"), "; ")
			}
		}
		out = append(out, e)
	}
	if out == nil {
		out = []MappingEntry{}
	}
	return out, nil
}

// ApplyMapping adds the resolved senders to their bindings; entries with an error are skipped
// and counted.
func (s *Service) ApplyMapping(ctx context.Context, entries []MappingEntry) (added, skipped int, err error) {
	for _, e := range entries {
		if e.Error != "" || e.Sender == nil || e.Binding == "" {
			skipped++
			continue
		}
		b, ok := s.o.Store.Get(e.Binding)
		if !ok {
			skipped++
			continue
		}
		dup := false
		for _, snd := range b.Senders {
			if snd.Interface == e.Sender.Interface && snd.Address == e.Sender.Address && snd.Role == e.Sender.Role {
				dup = true
			}
		}
		if dup {
			skipped++
			continue
		}
		b.Senders = append(b.Senders, *e.Sender)
		if verr := b.Validate(); verr != nil {
			skipped++
			if err == nil {
				err = verr
			}
			continue
		}
		if perr := s.o.Store.Put(b); perr != nil {
			return added, skipped, perr
		}
		added++
		s.reportUsages(ctx, b.ID)
	}
	s.reload()
	s.broadcast(Event{Type: "binding", State: "updated", TS: s.o.Now()})
	return added, skipped, err
}

// --- the UI stream -------------------------------------------------------------------------

// Subscribe answers a channel of events; the caller calls cancel when done.
func (s *Service) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Service) broadcast(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Relay exposes the relay (tests).
func (s *Service) Relay() *relay.Relay { return s.relay }
