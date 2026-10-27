package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/relay"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/store"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/system"
)

func hmipDefault(iface, receiver string) map[string]json.RawMessage {
	m := map[string]any{
		"LONG_PROFILE_ACTION_TYPE": 5, "SHORT_PROFILE_ACTION_TYPE": 1, "LONG_MULTIEXECUTE": true,
		"LONG_JT_OFF": 1, "LONG_JT_ONDELAY": 2, "LONG_JT_RAMPON": 3, "LONG_JT_ON": 4, "LONG_JT_OFFDELAY": 5, "LONG_JT_RAMPOFF": 6,
		"SHORT_JT_OFF": 1, "SHORT_JT_ONDELAY": 2, "SHORT_JT_RAMPON": 3, "SHORT_JT_ON": 4, "SHORT_JT_OFFDELAY": 5, "SHORT_JT_RAMPOFF": 6,
		"LONG_DIM_STEP": 0.05, "LONG_DIM_MIN_LEVEL": 0.0, "LONG_DIM_MAX_LEVEL": 1.0, "SHORT_ON_LEVEL": 1.0, "UI_HINT": "3",
	}
	out := map[string]json.RawMessage{}
	for k, v := range m {
		b, _ := json.Marshal(v)
		out[k] = b
	}
	return out
}

func newFake() *system.Fake {
	f := system.NewFake()
	f.AddVirtualRemote("HmIP-RF")
	f.AddVirtualRemote("BidCos-RF")
	f.AddVirtualRemote("BidCos-Wired")
	f.AddDevice("HmIP-RF", "PDT", "HmIP-PDT", "DIMMER_VIRTUAL_RECEIVER", "DIMMER_VIRTUAL_RECEIVER", "DIMMER_VIRTUAL_RECEIVER")
	f.AddDevice("BidCos-RF", "RC", "HM-RC-19", "KEY", "KEY", "KEY")
	f.AddDevice("HmIP-RF", "WRC", "HmIP-WRC2", "KEY_TRANSCEIVER", "KEY_TRANSCEIVER")
	f.AddDevice("BidCos-Wired", "DIM", "HMW-LC-Dim1L-DR", "KEY", "KEY", "HMW_DIMMER")
	f.DefaultParamset = hmipDefault
	f.Meta["HmIP-RF.PDT:3"] = system.Object{Name: "Licht Küche", Rooms: []string{"Küche"}}
	f.Meta["BidCos-RF.RC:1"] = system.Object{Name: "Taste 1"}
	f.Meta["BidCos-RF.RC"] = system.Object{Name: "RC19"}
	f.Desc["BidCos-RF"] = litesource.ParamsetCache{"key": {"PRESS_SHORT": {}, "PRESS_LONG": {}}}
	f.DescKeys["BidCos-RF|RC:1"], f.DescKeys["BidCos-RF|RC:2"], f.DescKeys["BidCos-RF|RC:3"] = "key", "key", "key"
	return f
}

func newService(t *testing.T, f *system.Fake) *Service {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{System: f, Store: st, Settings: store.OpenSettings(dir), Log: func(string, ...any) {}, PendingEvery: 10 * time.Millisecond, CheckEvery: time.Hour})
}

func draft() model.Binding {
	return model.Binding{Target: model.Target{Interface: "HmIP-RF", Address: "PDT:3"}, Senders: []model.Sender{{Interface: "BidCos-RF", Address: "RC:1", Role: model.RoleUp}, {Interface: "BidCos-RF", Address: "RC:2", Role: model.RoleDown}}}
}

func TestPlanApplyRemove(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	p, err := s.Plan(ctx, draft())
	if err != nil {
		t.Fatal(err)
	}
	b := p.Binding
	if b.ID != "pdt-3" || b.Up != "HmIP-RCV-1:50" || b.Down != "HmIP-RCV-1:49" || b.Mode != model.TwoKey || b.Target.Family != profiles.HmIPDimmer {
		t.Fatalf("completed %+v", b)
	}
	if len(f.CallsOf("addLink")) != 0 {
		t.Error("the plan wrote")
	}
	r, err := s.Apply(ctx, draft())
	if err != nil || r.Failed != "" {
		t.Fatalf("apply %v %+v", err, r)
	}
	views, _ := s.Bindings(ctx)
	if len(views) != 1 || views[0].TargetName != "Licht Küche" || views[0].UpName != "Licht Küche ▲" || views[0].SenderViews[0].Name != "Taste 1" || views[0].Status.State != StateActive || views[0].Kind != profiles.Dimmer || views[0].TargetDevice != "HmIP-PDT" {
		t.Fatalf("views %+v", views)
	}
	// the relay knows it: a press on the sender reaches the key
	s.relay.Handle(ctx, relayPress("BidCos-RF", "RC:1", "PRESS_LONG"))
	waitCalls(t, f, "setValue", 1)
	if c := f.CallsOf("setValue")[0]; c != "HmIP-RF setValue HmIP-RCV-1:50 PRESS_LONG true" {
		t.Errorf("relayed %s", c)
	}
	// the next binding takes the next keys and a numbered id
	d2 := draft()
	d2.Target.Address = "PDT:2"
	if r, err := s.Apply(ctx, d2); err != nil {
		t.Fatal(err)
	} else if r.Plan.Binding.Up != "HmIP-RCV-1:48" || r.Plan.Binding.ID != "pdt-2" {
		t.Errorf("second %+v", r.Plan.Binding)
	}
	// a changed sender list needs no link write
	v, err := s.SetSenders(ctx, "pdt-3", []model.Sender{{Interface: "BidCos-RF", Address: "RC:3", Role: model.RoleBoth}}, 400)
	if err != nil || len(v.Senders) != 1 || v.StepIntervalMS != 400 {
		t.Fatalf("set senders %v %+v", err, v)
	}
	if _, err := s.SetSenders(ctx, "pdt-3", []model.Sender{{Interface: "HmIP-RF", Address: "PDT:1", Role: model.RoleUp}}, 0); err == nil {
		t.Error("same-interface sender accepted")
	}
	if _, err := s.SetSenders(ctx, "nope", nil, 0); !errors.Is(err, ErrNotFound) {
		t.Error("unknown id")
	}
	// keys cannot change
	b.Up = "HmIP-RCV-1:40"
	if _, err := s.Plan(ctx, b); err == nil || !strings.Contains(err.Error(), "keys cannot be changed") {
		t.Errorf("key change: %v", err)
	}
	// remove: links and markers go
	n := len(f.CallsOf("removeLink"))
	rp, err := s.PlanRemove(ctx, "pdt-3", false)
	if err != nil || len(rp.Steps) != 4 {
		t.Fatalf("remove plan %v %+v", err, rp)
	}
	if _, err := s.Remove(ctx, "pdt-3", false); err != nil {
		t.Fatal(err)
	}
	if len(f.CallsOf("removeLink")) != n+2 {
		t.Error("links not removed")
	}
	if _, ok := s.Binding(ctx, "pdt-3"); ok {
		t.Error("still there")
	}
	if _, err := s.Remove(ctx, "pdt-3", false); !errors.Is(err, ErrNotFound) {
		t.Error("second remove")
	}
	removed, err := s.RemoveAll(ctx, true)
	if err != nil || removed != 1 {
		t.Errorf("remove all %d %v", removed, err)
	}
	links, _ := system.Links(ctx, f, "HmIP-RF")
	if len(links) != 2 {
		t.Error("forget removed the links")
	}
}

func relayPress(iface, addr, key string) relay.Press {
	return relay.Press{Interface: iface, Address: addr, Key: key}
}

func waitCalls(t *testing.T, f *system.Fake, method string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(f.CallsOf(method)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%s not called %d times: %v", method, n, f.Calls)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRefusals(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	// a switch is not a target
	d := draft()
	d.Target.Address = "PDT:0"
	if _, err := s.Plan(ctx, d); err == nil {
		t.Error("channel 0 planned")
	}
	d.Target.Address = "NOPE:1"
	if _, err := s.Plan(ctx, d); err == nil {
		t.Error("unknown channel planned")
	}
	// a key in use is refused
	f.LinksOf["HmIP-RF"] = []system.Link{{Sender: "HmIP-RCV-1:50", Receiver: "PDT:1"}}
	d = draft()
	d.Up = "HmIP-RCV-1:50"
	if _, err := s.Plan(ctx, d); err == nil || !strings.Contains(err.Error(), "linked already") {
		t.Errorf("linked key: %v", err)
	}
	f.Meta["HmIP-RF.HmIP-RCV-1:47"] = system.Object{Name: "Flow key"}
	d.Up = "HmIP-RCV-1:47"
	if _, err := s.Plan(ctx, d); err == nil || !strings.Contains(err.Error(), "named") {
		t.Errorf("named key: %v", err)
	}
	d.Up = "HmIP-RCV-1:99"
	if _, err := s.Plan(ctx, d); err == nil {
		t.Error("key 99")
	}
	// no free keys left
	for n := 1; n <= 50; n++ {
		f.Meta["HmIP-RF.HmIP-RCV-1:"+itoa(n)] = system.Object{Name: "x"}
	}
	if _, err := s.Plan(ctx, draft()); err == nil || !strings.Contains(err.Error(), "free") {
		t.Errorf("no free keys: %v", err)
	}
	// a system error
	f.Fail["getLinks"] = errors.New("down")
	if _, err := s.Plan(ctx, draft()); err == nil {
		t.Error("getLinks failure")
	}
	if _, err := s.Keys(ctx, "VirtualDevices"); err == nil {
		t.Error("keys of VirtualDevices")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestApplyFailureKeepsCreatedLinks(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	f.Fail["setObject"] = errors.New("403")
	r, err := s.Apply(ctx, draft())
	if err == nil || r == nil {
		t.Fatalf("no failure: %v %+v", err, r)
	}
	// the link exists: the binding is stored with an error status, so the user can retry
	v, ok := s.Binding(ctx, "pdt-3")
	if !ok || v.Status.State != StateError {
		t.Fatalf("binding %+v %v", v, ok)
	}
	delete(f.Fail, "setObject")
	r, err = s.Reapply(ctx, "pdt-3")
	if err != nil || r.Failed != "" {
		t.Fatalf("reapply %v %+v", err, r)
	}
	if v, _ := s.Binding(ctx, "pdt-3"); v.Status.State != StateActive {
		t.Errorf("after reapply %+v", v.Status)
	}
	if _, err := s.Reapply(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Error("reapply unknown")
	}
	// a plan failure before any write stores nothing
	f.Fail["addLink"] = errors.New("nope")
	d := draft()
	d.Target.Address = "PDT:1"
	if _, err := s.Apply(ctx, d); err == nil {
		t.Error("addLink failure")
	}
	if _, ok := s.Binding(ctx, "pdt-1"); ok {
		t.Error("stored without links")
	}
}

func TestCheckStates(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	if _, err := s.Apply(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	// changed elsewhere
	f.SetParamset("HmIP-RF", "PDT:3", "HmIP-RCV-1:50", map[string]any{"LONG_PROFILE_ACTION_TYPE": 5})
	s.Check(ctx)
	v, _ := s.Binding(ctx, "pdt-3")
	if v.Status.State != StateChanged || v.Status.Differs["HmIP-RCV-1:50"] == nil {
		t.Errorf("changed: %+v", v.Status)
	}
	// pending while the device has not taken it
	f.SetPending("HmIP-RF", "PDT", true)
	s.Check(ctx)
	if v, _ := s.Binding(ctx, "pdt-3"); v.Status.State != StatePending {
		t.Errorf("pending: %+v", v.Status)
	}
	if h := s.Health(); h.Pending != 1 || h.Bindings != 1 {
		t.Errorf("health %+v", h)
	}
	// reapply heals it
	f.SetPending("HmIP-RF", "PDT", false)
	if _, err := s.Reapply(ctx, "pdt-3"); err != nil {
		t.Fatal(err)
	}
	s.Check(ctx)
	if v, _ := s.Binding(ctx, "pdt-3"); v.Status.State != StateActive {
		t.Errorf("active: %+v", v.Status)
	}
	// a link gone
	f.LinksOf["HmIP-RF"] = f.LinksOf["HmIP-RF"][:1]
	s.Check(ctx)
	if v, _ := s.Binding(ctx, "pdt-3"); v.Status.State != StateMissing || len(v.Status.Missing) != 1 {
		t.Errorf("missing: %+v", v.Status)
	}
	if h := s.Health(); h.Changed != 1 {
		t.Errorf("health %+v", h)
	}
	// the system down
	f.Fail["getLinks"] = errors.New("down")
	s.Check(ctx)
	if v, _ := s.Binding(ctx, "pdt-3"); v.Status.State != StateError {
		t.Errorf("error: %+v", v.Status)
	}
	delete(f.Fail, "getLinks")
	f.Fail["getParamset"] = errors.New("down")
	f.LinksOf["HmIP-RF"] = append(f.LinksOf["HmIP-RF"], system.Link{Sender: "HmIP-RCV-1:49", Receiver: "PDT:3"})
	s.Check(ctx)
	if v, _ := s.Binding(ctx, "pdt-3"); v.Status.State != StateError {
		t.Errorf("read error: %+v", v.Status)
	}
}

func TestRunStreamAndChecker(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := s.Apply(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	ev, unsub := s.Subscribe()
	defer unsub()
	go s.Run(ctx)
	f.Events <- litesource.Message{Type: "hello", Hello: &litesource.Hello{}}
	f.Emit("BidCos-RF", "RC:2", "PRESS_LONG", true)
	waitCalls(t, f, "setValue", 1)
	if c := f.CallsOf("setValue")[0]; c != "HmIP-RF setValue HmIP-RCV-1:49 PRESS_LONG true" {
		t.Errorf("relayed %s", c)
	}
	f.Emit("HmIP-RF", "PDT:3", "LEVEL", 0.42)
	var seen []string
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case e := <-ev:
			if e.Type == "press" || e.Type == "level" {
				seen = append(seen, e.Type)
			}
		case <-deadline:
			t.Fatalf("events %v", seen)
		}
	}
	if a, _ := s.relay.Activity("pdt-3"); a.Level != 0.42 {
		t.Errorf("level %+v", a)
	}
	// a device message triggers a check; the health shows the stream
	f.Events <- litesource.Message{Type: "updateDevice", Devices: &litesource.DeviceChange{Interface: "HmIP-RF", Hint: 1}}
	f.Events <- litesource.Message{Type: "interface", Interface: &litesource.InterfaceChange{Interface: "HmIP-RF", State: "up"}}
	f.Events <- litesource.Message{Type: "resync", Resync: &litesource.Resync{Reason: "boot"}}
	time.Sleep(50 * time.Millisecond)
	if h := s.Health(); !h.Stream || h.Presses != 1 {
		t.Errorf("health %+v", h)
	}
	// the stream ends: the service reconnects
	close(f.Events)
	time.Sleep(20 * time.Millisecond)
	if h := s.Health(); h.Stream {
		t.Error("stream still up")
	}
}

func TestForeignAdoptAndImport(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	// the Node-RED era: two links on the wired dimmer with the WebUI's profiles, keys named
	f.LinksOf["BidCos-Wired"] = []system.Link{{Sender: "BidCoS-Wir:31", Receiver: "DIM:3", Name: "DimUp"}, {Sender: "BidCoS-Wir:30", Receiver: "DIM:3", Name: "DimDown"}, {Sender: "BidCoS-Wir:1", Receiver: "DIM:1"}}
	up, _ := profiles.Params(profiles.WiredDimmer, profiles.UpOn, profiles.Options{Step: 0.03})
	down, _ := profiles.Params(profiles.WiredDimmer, profiles.DownOff, profiles.Options{Step: 0.03})
	f.SetParamset("BidCos-Wired", "DIM:3", "BidCoS-Wir:31", up)
	f.SetParamset("BidCos-Wired", "DIM:3", "BidCoS-Wir:30", down)
	f.Meta["BidCos-Wired.BidCoS-Wir:31"] = system.Object{Name: "Licht Esstisch DimUp"}
	f.Meta["BidCos-Wired.BidCoS-Wir:30"] = system.Object{Name: "Licht Esstisch DimDown"}
	f.Meta["BidCos-Wired.DIM:3"] = system.Object{Name: "Licht Esstisch"}
	fl, err := s.Foreign(ctx)
	if err != nil || len(fl) != 2 || fl[0].Key != "BidCoS-Wir:30" || fl[0].Profile.Role != profiles.DownOff || fl[1].Profile.Step != 0.03 {
		t.Fatalf("foreign %v %+v", err, fl)
	}
	// the mapping: the first line resolves, the second names an unknown channel, the third a
	// key no binding owns yet
	text := "const map = {\n 'RC19:1': 'Licht Esstisch DimUp',\n \"Nope:2\": \"Licht Esstisch DimDown\",\n 'Taste 1': 'Licht Esstisch DimDown'\n};"
	entries, err := s.ParseMapping(ctx, text)
	if err != nil || len(entries) != 3 {
		t.Fatalf("parse %v %+v", err, entries)
	}
	if entries[0].Sender == nil || entries[0].Sender.Address != "RC:1" || !strings.Contains(entries[0].Error, "adopt") || entries[0].Key != "BidCoS-Wir:31" {
		t.Errorf("entry 0 %+v", entries[0])
	}
	if entries[1].Sender != nil || !strings.Contains(entries[1].Error, "no channel named") {
		t.Errorf("entry 1 %+v", entries[1])
	}
	// adopt: marking only, the step from the link
	n := len(f.CallsOf("putParamset"))
	r, err := s.Adopt(ctx, "BidCos-Wired", "DIM:3", "BidCoS-Wir:31", "BidCoS-Wir:30", nil)
	if err != nil || r.Failed != "" {
		t.Fatalf("adopt %v %+v", err, r)
	}
	if len(f.CallsOf("putParamset")) != n || len(f.CallsOf("addLink")) != 0 {
		t.Error("adopt wrote links")
	}
	v, _ := s.Binding(ctx, "dim-3")
	if !v.Adopted || v.Options.Step != 0.03 || v.Mode != model.TwoKey || v.UpName != "Licht Esstisch DimUp" || v.Status.State != StateActive {
		t.Errorf("adopted %+v", v)
	}
	if o := f.Meta["BidCos-Wired.BidCoS-Wir:31"]; o.Name != "Licht Esstisch DimUp" || o.Marker() == nil || o.Marker().Binding != "dim-3" {
		t.Errorf("marker %+v", o)
	}
	if fl, _ := s.Foreign(ctx); len(fl) != 0 {
		t.Errorf("still foreign %+v", fl)
	}
	// the mapping now resolves the binding, apply adds the senders
	entries, _ = s.ParseMapping(ctx, text)
	if entries[0].Error != "" || entries[0].Binding != "dim-3" || entries[0].Role != "up" || entries[2].Role != "down" {
		t.Errorf("entries %+v", entries)
	}
	added, skipped, err := s.ApplyMapping(ctx, entries)
	if err != nil || added != 2 || skipped != 1 {
		t.Errorf("apply mapping %d %d %v", added, skipped, err)
	}
	added, skipped, _ = s.ApplyMapping(ctx, entries)
	if added != 0 || skipped != 3 {
		t.Errorf("duplicates %d %d", added, skipped)
	}
	v, _ = s.Binding(ctx, "dim-3")
	if len(v.Senders) != 2 || v.Senders[0].Role != "up" || v.Senders[1].Address != "RC:1" {
		t.Errorf("senders %+v", v.Senders)
	}
	// an adopted binding's drift is the direction only
	f.SetParamset("BidCos-Wired", "DIM:3", "BidCoS-Wir:31", map[string]any{"LONG_ACTION_TYPE": 5})
	s.Check(ctx)
	if v, _ := s.Binding(ctx, "dim-3"); v.Status.State != StateChanged {
		t.Errorf("adopted drift %+v", v.Status)
	}
	// not adoptable: a wrong direction, a missing link, a switch
	if _, err := s.Adopt(ctx, "BidCos-Wired", "DIM:3", "BidCoS-Wir:30", "BidCoS-Wir:31", nil); err == nil {
		t.Error("swapped keys adopted")
	}
	if _, err := s.Adopt(ctx, "BidCos-Wired", "DIM:3", "BidCoS-Wir:31", "BidCoS-Wir:2", nil); err == nil {
		t.Error("missing link adopted")
	}
	if _, err := s.Adopt(ctx, "BidCos-Wired", "DIM:1", "BidCoS-Wir:1", "BidCoS-Wir:2", nil); err == nil {
		t.Error("key channel adopted")
	}
	// forget leaves the links and the names
	if _, err := s.Remove(ctx, "dim-3", true); err != nil {
		t.Fatal(err)
	}
	if len(f.LinksOf["BidCos-Wired"]) != 3 || f.Meta["BidCos-Wired.BidCoS-Wir:31"].Name != "Licht Esstisch DimUp" {
		t.Error("forget removed something")
	}
}

func TestSettingsTargetsSendersPress(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	st, err := s.SetSettings(model.Settings{StepIntervalMS: 500, LogPresses: true})
	if err != nil || st.StepIntervalMS != 500 || st.HoldTimeoutMS != 1000 || s.Settings() != st {
		t.Errorf("settings %+v %v", st, err)
	}
	ts, err := s.Targets(ctx)
	if err != nil || len(ts) != 4 {
		t.Errorf("targets %v %+v", err, ts)
	}
	ss, err := s.Senders(ctx)
	if err != nil || len(ss) != 3 || ss[0].Name != "Taste 1" {
		t.Errorf("senders %v %+v", err, ss)
	}
	ifs, _ := s.Interfaces(ctx)
	if len(ifs) != 3 {
		t.Errorf("interfaces %+v", ifs)
	}
	if _, err := s.Apply(ctx, draft()); err != nil {
		t.Fatal(err)
	}
	if err := s.TestPress(ctx, "pdt-3", "down", "PRESS_SHORT"); err != nil {
		t.Error(err)
	}
	if c := f.CallsOf("setValue"); len(c) != 1 || c[0] != "HmIP-RF setValue HmIP-RCV-1:49 PRESS_SHORT true" {
		t.Errorf("press %v", c)
	}
	if err := s.TestPress(ctx, "pdt-3", "up", "LEVEL"); err == nil {
		t.Error("LEVEL pressed")
	}
	if err := s.TestPress(ctx, "x", "up", "PRESS_LONG"); !errors.Is(err, ErrNotFound) {
		t.Error("unknown pressed")
	}
	keys, err := s.Keys(ctx, "HmIP-RF")
	if err != nil || len(keys) != 50 || keys[49].Free || keys[49].Marker == nil {
		t.Errorf("keys %v", err)
	}
	f.Fail["interfaces"] = errors.New("down")
	s.ifaces = nil
	if _, err := s.Targets(ctx); err == nil {
		t.Error("targets without interfaces")
	}
}

func TestOneKeyBindingProfiles(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	d := draft()
	d.Mode = model.OneKey
	d.Senders = []model.Sender{{Interface: "BidCos-RF", Address: "RC:1", Role: model.RoleBoth}}
	d.Options = profiles.Options{OnLevel: profiles.LastLevel, Step: 0.1}
	r, err := s.Apply(ctx, d)
	if err != nil || r.Failed != "" {
		t.Fatal(err)
	}
	up := profiles.Classify(profiles.HmIPDimmer, f.Paramset("HmIP-RF", "PDT:3", "HmIP-RCV-1:50"))
	down := profiles.Classify(profiles.HmIPDimmer, f.Paramset("HmIP-RF", "PDT:3", "HmIP-RCV-1:49"))
	if up.Role != profiles.UpToggle || up.OnLevel != profiles.LastLevel || up.Step != 0.1 || down.Role != profiles.DownOnly {
		t.Errorf("profiles %+v %+v", up, down)
	}
}

func TestValueUsages(t *testing.T) {
	f := newFake()
	s := newService(t, f)
	ctx := context.Background()
	// a wired dimmer with an HmIP key: the key's presses need a registered usage
	d := model.Binding{Target: model.Target{Interface: "BidCos-Wired", Address: "DIM:3"}, Senders: []model.Sender{{Interface: "HmIP-RF", Address: "WRC:1", Role: model.RoleUp}, {Interface: "BidCos-RF", Address: "RC:1", Role: model.RoleDown}}}
	if _, err := s.Apply(ctx, d); err != nil {
		t.Fatal(err)
	}
	if f.Usages["HmIP-RF|WRC:1|PRESS_SHORT"] != 1 || f.Usages["HmIP-RF|WRC:1|PRESS_LONG"] != 1 || len(f.Usages) != 2 {
		t.Fatalf("usages %v", f.Usages)
	}
	v, _ := s.Binding(ctx, "dim-3")
	if v.Senders[0].Usage == nil || v.Senders[0].Usage.State != model.UsageReported || v.Senders[1].Usage != nil {
		t.Fatalf("senders %+v", v.Senders)
	}
	n := len(f.CallsOf("reportValueUsage"))
	s.Check(ctx)
	if len(f.CallsOf("reportValueUsage")) != n {
		t.Error("a reported usage was reported again")
	}
	// a second binding with the same key: removing the first keeps the usage
	d2 := d
	d2.Target.Address = "DIM:3"
	d2.ID = "second"
	d2.Senders = []model.Sender{{Interface: "HmIP-RF", Address: "WRC:1", Role: model.RoleBoth}}
	if _, err := s.Apply(ctx, d2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove(ctx, "dim-3", true); err != nil {
		t.Fatal(err)
	}
	if f.Usages["HmIP-RF|WRC:1|PRESS_SHORT"] != 1 {
		t.Error("released although another binding uses the key")
	}
	// the senders change: the one that goes is released, a new one reported
	if _, err := s.SetSenders(ctx, "second", []model.Sender{{Interface: "HmIP-RF", Address: "WRC:2", Role: model.RoleBoth}}, 0); err != nil {
		t.Fatal(err)
	}
	if f.Usages["HmIP-RF|WRC:1|PRESS_SHORT"] != 0 || f.Usages["HmIP-RF|WRC:2|PRESS_LONG"] != 1 {
		t.Errorf("usages after the change %v", f.Usages)
	}
	// the settings keep them
	s.SetSettings(model.Settings{KeepUsages: true})
	if _, err := s.Remove(ctx, "second", true); err != nil {
		t.Fatal(err)
	}
	if f.Usages["HmIP-RF|WRC:2|PRESS_SHORT"] != 1 {
		t.Error("released although the settings keep usages")
	}
	// pending: the process answers false; the check reports again
	s.SetSettings(model.Settings{})
	f.UsagePending = true
	if _, err := s.Apply(ctx, d); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Binding(ctx, "dim-3")
	if v.Senders[0].Usage.State != model.UsagePending || !s.anyPending() {
		t.Errorf("pending %+v %v", v.Senders[0].Usage, f.CallsOf("reportValueUsage"))
	}
	f.UsagePending = false
	s.Check(ctx)
	v, _ = s.Binding(ctx, "dim-3")
	if v.Senders[0].Usage.State != model.UsageReported {
		t.Errorf("after the check %+v", v.Senders[0].Usage)
	}
	// failed
	f.Fail["reportValueUsage"] = errors.New("down")
	if _, err := s.SetSenders(ctx, "dim-3", []model.Sender{{Interface: "HmIP-RF", Address: "WRC:2", Role: model.RoleUp}}, 0); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Binding(ctx, "dim-3")
	if v.Senders[0].Usage.State != model.UsageFailed {
		t.Errorf("failed %+v", v.Senders[0].Usage)
	}
}
