// Package relay forwards a sender's presses to a binding's virtual keys. It normalises the
// senders' event shapes (BidCos-RF PRESS_LONG / PRESS_CONT / PRESS_LONG_RELEASE, HmIP
// PRESS_LONG_START / PRESS_LONG / PRESS_LONG_RELEASE, wired and some remotes PRESS_LONG only)
// into short, hold-begin, hold-repeat and hold-end; mirrors the repeats as one PRESS_LONG on the
// direction's key at most every step interval, dropping the surplus (a queued press overshoots);
// and keeps the direction of a one-key hold: up and down in turns, the actuator's last reported
// level deciding at the ends. Every relayed press is a new press on the actuator - one step - so
// a hold's end needs nothing: when the presses stop, the actuator stops.
package relay

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
)

// Press is a normalised sender event.
type Press struct {
	Interface string
	Address   string
	// Key is the datapoint: PRESS_SHORT, PRESS_LONG, PRESS_CONT, PRESS_LONG_START,
	// PRESS_LONG_RELEASE.
	Key string
	// TS is the event's time; zero when unknown (then it is taken as now).
	TS time.Time
}

// Emitter presses a virtual key.
type Emitter interface {
	Press(ctx context.Context, iface, key, datapoint string) error
}

// EmitFunc adapts a function.
type EmitFunc func(ctx context.Context, iface, key, datapoint string) error

// Press implements Emitter.
func (f EmitFunc) Press(ctx context.Context, iface, key, datapoint string) error {
	return f(ctx, iface, key, datapoint)
}

// Options configure the relay.
type Options struct {
	Emit Emitter
	// Interval between two relayed long presses of one binding; 300 ms by default.
	Interval time.Duration
	// HoldTimeout: a hold without a release ends this long after its last repeat; 1 s.
	HoldTimeout time.Duration
	// MaxAge: an event older than this (a replay after a reconnect) is dropped; 1.5 s.
	MaxAge time.Duration
	// ShortDedup: a second short press of the same sender within this is dropped; 300 ms.
	ShortDedup time.Duration
	Log        func(string, ...any)
	LogPresses bool
	Now        func() time.Time
}

// Activity is what the relay last did for a binding.
type Activity struct {
	// LastHold is when the last hold began, zero when none yet.
	LastHold time.Time `json:"last_hold,omitempty"`
	// LastDirection of the last hold.
	LastDirection string `json:"last_direction,omitempty"`
	// Steps relayed during the last hold.
	Steps int `json:"steps"`
	// Dropped presses during the last hold (surplus inside the interval).
	Dropped int `json:"dropped"`
	// LastShort is when the last short press was relayed.
	LastShort time.Time `json:"last_short,omitempty"`
	// Holding tells whether a hold is in progress.
	Holding bool `json:"holding"`
	// Level is the actuator's last reported level, -1 when none.
	Level float64 `json:"level"`
	// Errors counts failed presses since the start.
	Errors int `json:"errors"`
}

type state struct {
	b        model.Binding
	interval time.Duration
	// the hold
	holding   bool
	dir       string
	lastEvent time.Time
	lastSend  time.Time
	inflight  bool
	// the one-key direction memory: the direction of the last hold
	lastDir string
	level   float64 // -1 unknown
	act     Activity
	// per sender: the last short press relayed
	lastShort map[string]time.Time
}

// Relay routes presses.
type Relay struct {
	o  Options
	mu sync.Mutex
	// bindings by id
	st map[string]*state
	// senders maps iface|address to the bindings (ids) and the sender's role per binding
	senders map[string][]senderRef
	// targets maps iface|address to the binding ids (for level reports)
	targets map[string][]string
}

type senderRef struct {
	id   string
	role string
}

// New makes a relay.
func New(o Options) *Relay {
	if o.Interval <= 0 {
		o.Interval = 300 * time.Millisecond
	}
	if o.HoldTimeout <= 0 {
		o.HoldTimeout = time.Second
	}
	if o.MaxAge <= 0 {
		o.MaxAge = 1500 * time.Millisecond
	}
	if o.ShortDedup <= 0 {
		o.ShortDedup = 300 * time.Millisecond
	}
	if o.Log == nil {
		o.Log = log.Printf
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Relay{o: o, st: map[string]*state{}, senders: map[string][]senderRef{}, targets: map[string][]string{}}
}

// Configure changes the timing and logging (the settings page); zero keeps a value.
func (r *Relay) Configure(interval, holdTimeout time.Duration, logPresses bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if interval > 0 {
		r.o.Interval = interval
	}
	if holdTimeout > 0 {
		r.o.HoldTimeout = holdTimeout
	}
	r.o.LogPresses = logPresses
}

// SetBindings replaces the bindings; the state of the ones that stay (direction memory, level)
// is kept. interval is the settings' step interval, a binding's own overrides it.
func (r *Relay) SetBindings(bs []model.Binding, interval time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if interval <= 0 {
		interval = r.o.Interval
	}
	next := map[string]*state{}
	r.senders = map[string][]senderRef{}
	r.targets = map[string][]string{}
	for _, b := range bs {
		s, ok := r.st[b.ID]
		if !ok {
			s = &state{level: -1, lastShort: map[string]time.Time{}, act: Activity{Level: -1}}
		}
		s.b = b
		s.interval = interval
		if b.StepIntervalMS > 0 {
			s.interval = time.Duration(b.StepIntervalMS) * time.Millisecond
		}
		next[b.ID] = s
		for _, snd := range b.Senders {
			k := snd.Interface + "|" + snd.Address
			r.senders[k] = append(r.senders[k], senderRef{id: b.ID, role: snd.Role})
		}
		tk := b.Target.Interface + "|" + b.Target.Address
		r.targets[tk] = append(r.targets[tk], b.ID)
	}
	r.st = next
}

// Addresses answers the sender and target addresses per interface the relay wants on the stream.
func (r *Relay) Addresses() map[string][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	out := map[string][]string{}
	add := func(iface, addr string) {
		if !seen[iface+"|"+addr] {
			seen[iface+"|"+addr] = true
			out[iface] = append(out[iface], addr)
		}
	}
	for _, s := range r.st {
		for _, snd := range s.b.Senders {
			add(snd.Interface, snd.Address)
		}
		add(s.b.Target.Interface, s.b.Target.Address)
	}
	return out
}

// Level takes an actuator's level report.
func (r *Relay) Level(iface, address string, level float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.targets[iface+"|"+address] {
		if s := r.st[id]; s != nil {
			s.level = level
			s.act.Level = level
		}
	}
}

// Activity answers a binding's activity.
func (r *Relay) Activity(id string) (Activity, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.st[id]
	if !ok {
		return Activity{}, false
	}
	a := s.act
	a.Holding = s.holding && r.o.Now().Sub(s.lastEvent) <= r.o.HoldTimeout
	return a, true
}

// IsPress tells whether a datapoint is one the relay handles.
func IsPress(key string) bool {
	return strings.HasPrefix(key, "PRESS_")
}

// Handle takes a sender's press.
func (r *Relay) Handle(ctx context.Context, p Press) {
	now := r.o.Now()
	if !p.TS.IsZero() && now.Sub(p.TS) > r.o.MaxAge {
		return
	}
	r.mu.Lock()
	refs := r.senders[p.Interface+"|"+p.Address]
	r.mu.Unlock()
	for _, ref := range refs {
		r.handleOne(ctx, ref, p, now)
	}
}

func (r *Relay) handleOne(ctx context.Context, ref senderRef, p Press, now time.Time) {
	r.mu.Lock()
	s := r.st[ref.id]
	if s == nil {
		r.mu.Unlock()
		return
	}
	sender := p.Interface + "|" + p.Address
	switch p.Key {
	case "PRESS_SHORT":
		if t, ok := s.lastShort[sender]; ok && now.Sub(t) < r.o.ShortDedup {
			r.mu.Unlock()
			return
		}
		s.lastShort[sender] = now
		s.act.LastShort = now
		key := s.b.Up
		if ref.role == model.RoleDown {
			key = s.b.Down
		}
		iface := s.b.Target.Interface
		id := s.b.ID
		r.mu.Unlock()
		r.send(ctx, id, iface, key, "PRESS_SHORT")
	case "PRESS_LONG", "PRESS_CONT", "PRESS_LONG_START":
		if !s.holding || now.Sub(s.lastEvent) > r.o.HoldTimeout {
			// a hold begins
			s.holding = true
			s.dir = r.direction(s, ref.role)
			s.lastDir = s.dir
			s.act = Activity{LastHold: now, LastDirection: s.dir, Level: s.level, LastShort: s.act.LastShort, Errors: s.act.Errors}
		}
		s.lastEvent = now
		if s.inflight || now.Sub(s.lastSend) < s.interval {
			s.act.Dropped++
			r.mu.Unlock()
			return
		}
		s.inflight = true
		s.lastSend = now
		s.act.Steps++
		key := s.b.Key(s.dir)
		iface := s.b.Target.Interface
		id := s.b.ID
		r.mu.Unlock()
		r.send(ctx, id, iface, key, "PRESS_LONG")
	case "PRESS_LONG_RELEASE":
		s.holding = false
		r.mu.Unlock()
	default:
		r.mu.Unlock()
	}
}

// direction decides where a hold goes: a sender with a direction says it; a one-key sender
// alternates, the last reported level overriding at the ends (0: up, full: down).
func (r *Relay) direction(s *state, role string) string {
	switch role {
	case model.RoleUp:
		return "up"
	case model.RoleDown:
		return "down"
	}
	max := s.b.Options.MaxLevel
	if max <= 0 {
		max = 1
	}
	switch {
	case s.level >= 0 && s.level <= s.b.Options.MinLevel+1e-6:
		return "up"
	case s.level >= max-1e-6:
		return "down"
	case s.lastDir == "up":
		return "down"
	}
	return "up"
}

func (r *Relay) send(ctx context.Context, id, iface, key, datapoint string) {
	go func() {
		err := r.o.Emit.Press(ctx, iface, key, datapoint)
		r.mu.Lock()
		if s := r.st[id]; s != nil {
			if datapoint == "PRESS_LONG" {
				s.inflight = false
			}
			if err != nil {
				s.act.Errors++
			}
		}
		r.mu.Unlock()
		if err != nil {
			r.o.Log("longpressdim: %s %s on %s: %v", datapoint, key, iface, err)
		} else if r.o.LogPresses {
			r.o.Log("longpressdim: %s -> %s %s", id, key, datapoint)
		}
	}()
}
