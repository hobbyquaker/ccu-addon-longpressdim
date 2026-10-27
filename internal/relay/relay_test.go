package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
)

type recorder struct {
	mu   sync.Mutex
	sent []string
	fail bool
	// done is signalled after every press
	done  chan struct{}
	relay *Relay
}

func (r *recorder) Press(ctx context.Context, iface, key, datapoint string) error {
	r.mu.Lock()
	r.sent = append(r.sent, fmt.Sprintf("%s %s %s", iface, key, datapoint))
	fail := r.fail
	r.mu.Unlock()
	r.done <- struct{}{}
	if fail {
		return errors.New("boom")
	}
	return nil
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func setup(t *testing.T, bs ...model.Binding) (*Relay, *recorder, *clock) {
	t.Helper()
	rec := &recorder{done: make(chan struct{}, 100)}
	cl := &clock{t: time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)}
	var logs []string
	r := New(Options{Emit: rec, Now: cl.now, Log: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	rec.relay = r
	r.SetBindings(bs, 0)
	return r, rec, cl
}

func wait(t *testing.T, rec *recorder, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-rec.done:
		case <-time.After(2 * time.Second):
			t.Fatalf("press %d never came", i+1)
		}
	}
	// the relay clears its in-flight flag after the emitter returned: let it
	deadline := time.Now().Add(time.Second)
	for {
		rec.relay.mu.Lock()
		busy := false
		for _, s := range rec.relay.st {
			busy = busy || s.inflight
		}
		rec.relay.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a press stayed in flight")
		}
		time.Sleep(time.Millisecond)
	}
}

func twoKey() model.Binding {
	return model.Binding{ID: "b", Target: model.Target{Interface: "BidCos-Wired", Address: "DIM:3"}, Mode: model.TwoKey, Up: "BidCoS-Wir:50", Down: "BidCoS-Wir:49",
		Senders: []model.Sender{{Interface: "BidCos-RF", Address: "RC:1", Role: model.RoleUp}, {Interface: "BidCos-RF", Address: "RC:2", Role: model.RoleDown}}}
}

func oneKey() model.Binding {
	return model.Binding{ID: "o", Target: model.Target{Interface: "HmIP-RF", Address: "PDT:3"}, Mode: model.OneKey, Up: "HmIP-RCV-1:50", Down: "HmIP-RCV-1:49",
		Senders: []model.Sender{{Interface: "BidCos-RF", Address: "RC:3", Role: model.RoleBoth}}}
}

func TestTwoKeyHoldMirrorsAndThrottles(t *testing.T) {
	r, rec, cl := setup(t, twoKey())
	ctx := context.Background()
	press := func(addr, key string) { r.Handle(ctx, Press{Interface: "BidCos-RF", Address: addr, Key: key}) }
	// a BidCos-RF hold: PRESS_LONG, then PRESS_CONT every 250 ms for 1.5 s, then the release
	press("RC:1", "PRESS_LONG")
	wait(t, rec, 1)
	for i := 0; i < 6; i++ {
		cl.add(250 * time.Millisecond)
		press("RC:1", "PRESS_CONT")
		if i%2 == 1 {
			wait(t, rec, 1)
		}
	}
	cl.add(100 * time.Millisecond)
	press("RC:1", "PRESS_LONG_RELEASE")
	got := rec.list()
	// 0, 500, 1000, 1500 ms: four presses of seven events, every one on the up key
	if len(got) != 4 {
		t.Fatalf("sent %v", got)
	}
	for _, s := range got {
		if s != "BidCos-Wired BidCoS-Wir:50 PRESS_LONG" {
			t.Errorf("sent %s", s)
		}
	}
	a, _ := r.Activity("b")
	if a.Steps != 4 || a.Dropped != 3 || a.LastDirection != "up" || a.Holding {
		t.Errorf("activity %+v", a)
	}
	// the down sender's hold goes to the down key; a short press to its key
	cl.add(time.Second)
	press("RC:2", "PRESS_LONG")
	wait(t, rec, 1)
	press("RC:2", "PRESS_SHORT")
	wait(t, rec, 1)
	got = rec.list()
	if got[4] != "BidCos-Wired BidCoS-Wir:49 PRESS_LONG" || got[5] != "BidCos-Wired BidCoS-Wir:49 PRESS_SHORT" {
		t.Errorf("down %v", got[4:])
	}
	// a repeated short within 300 ms is dropped, after it goes through
	press("RC:2", "PRESS_SHORT")
	cl.add(400 * time.Millisecond)
	press("RC:1", "PRESS_SHORT")
	wait(t, rec, 1)
	got = rec.list()
	if len(got) != 7 || got[6] != "BidCos-Wired BidCoS-Wir:50 PRESS_SHORT" {
		t.Errorf("shorts %v", got[5:])
	}
	// an unknown sender does nothing; an old event is dropped
	press("RC:9", "PRESS_LONG")
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:1", Key: "PRESS_LONG", TS: cl.now().Add(-3 * time.Second)})
	if len(rec.list()) != 7 {
		t.Error("unknown or stale press relayed")
	}
}

func TestOneKeyAlternatesWithLevelOverride(t *testing.T) {
	r, rec, cl := setup(t, oneKey())
	ctx := context.Background()
	hold := func(repeats int) {
		r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG_START"})
		wait(t, rec, 1)
		for i := 0; i < repeats; i++ {
			cl.add(350 * time.Millisecond)
			r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG"})
			wait(t, rec, 1)
		}
		r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG_RELEASE"})
		cl.add(2 * time.Second)
	}
	dirs := func() []string {
		var out []string
		for _, s := range rec.list() {
			switch {
			case strings.Contains(s, ":50 PRESS_LONG"):
				out = append(out, "up")
			case strings.Contains(s, ":49 PRESS_LONG"):
				out = append(out, "down")
			}
		}
		return out
	}
	// no level known: up first, then alternating
	hold(1)
	hold(1)
	hold(0)
	if d := dirs(); strings.Join(d, ",") != "up,up,down,down,up" {
		t.Fatalf("dirs %v", d)
	}
	// the level says full: down, whatever came before
	r.Level("HmIP-RF", "PDT:3", 1.0)
	hold(0)
	// the level says off: up
	r.Level("HmIP-RF", "PDT:3", 0)
	hold(0)
	// mid-level: alternate from the last (up) -> down
	r.Level("HmIP-RF", "PDT:3", 0.5)
	hold(0)
	if d := dirs(); strings.Join(d[5:], ",") != "down,up,down" {
		t.Fatalf("dirs %v", d)
	}
	// a short press goes to the up key (the toggle)
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_SHORT"})
	wait(t, rec, 1)
	if got := rec.list(); got[len(got)-1] != "HmIP-RF HmIP-RCV-1:50 PRESS_SHORT" {
		t.Errorf("short %v", got[len(got)-1])
	}
	a, _ := r.Activity("o")
	if a.Level != 0.5 || a.LastDirection != "down" {
		t.Errorf("activity %+v", a)
	}
}

func TestHoldTimeoutStartsANewHold(t *testing.T) {
	// a wired key sends PRESS_LONG only; a second hold a while later alternates again
	r, rec, cl := setup(t, oneKey())
	ctx := context.Background()
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG"})
	wait(t, rec, 1)
	cl.add(400 * time.Millisecond)
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG"})
	wait(t, rec, 1)
	cl.add(1500 * time.Millisecond) // more than the hold timeout: a new hold
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG"})
	wait(t, rec, 1)
	got := rec.list()
	if !strings.Contains(got[0], ":50") || !strings.Contains(got[1], ":50") || !strings.Contains(got[2], ":49") {
		t.Errorf("%v", got)
	}
}

func TestInflightBlocksAndErrorsCount(t *testing.T) {
	rec := &recorder{done: make(chan struct{}, 100), fail: true}
	cl := &clock{t: time.Now()}
	var logs []string
	var mu sync.Mutex
	r := New(Options{Emit: rec, Now: cl.now, LogPresses: true, Log: func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }})
	rec.relay = r
	b := twoKey()
	b.StepIntervalMS = 500
	r.SetBindings([]model.Binding{b}, 300*time.Millisecond)
	ctx := context.Background()
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:1", Key: "PRESS_LONG"})
	wait(t, rec, 1)
	cl.add(400 * time.Millisecond) // inside the binding's own 500 ms
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:1", Key: "PRESS_CONT"})
	cl.add(200 * time.Millisecond)
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:1", Key: "PRESS_CONT"})
	wait(t, rec, 1)
	if len(rec.list()) != 2 {
		t.Errorf("sent %v", rec.list())
	}
	deadline := time.Now().Add(time.Second)
	for {
		a, _ := r.Activity("b")
		if a.Errors == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("errors %d", a.Errors)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	n := len(logs)
	mu.Unlock()
	if n < 2 {
		t.Errorf("logs %v", logs)
	}
	// configure changes the timing
	r.Configure(100*time.Millisecond, 2*time.Second, false)
	if r.o.Interval != 100*time.Millisecond || r.o.HoldTimeout != 2*time.Second || r.o.LogPresses {
		t.Error("configure")
	}
}

func TestSetBindingsKeepsStateAndAddresses(t *testing.T) {
	r, rec, cl := setup(t, oneKey(), twoKey())
	ctx := context.Background()
	r.Level("HmIP-RF", "PDT:3", 0.4)
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG"})
	wait(t, rec, 1)
	cl.add(2 * time.Second)
	// replace the list: the one-key binding stays (with its memory), the other goes
	r.SetBindings([]model.Binding{oneKey()}, 0)
	a, ok := r.Activity("o")
	if !ok || a.Level != 0.4 || a.LastDirection != "up" {
		t.Errorf("state lost %+v", a)
	}
	if _, ok := r.Activity("b"); ok {
		t.Error("removed binding still there")
	}
	addrs := r.Addresses()
	if len(addrs["BidCos-RF"]) != 1 || addrs["BidCos-RF"][0] != "RC:3" || len(addrs["HmIP-RF"]) != 1 {
		t.Errorf("addresses %v", addrs)
	}
	r.Handle(ctx, Press{Interface: "BidCos-RF", Address: "RC:3", Key: "PRESS_LONG"})
	wait(t, rec, 1)
	if got := rec.list(); !strings.Contains(got[1], ":49") {
		t.Errorf("memory not kept: %v", got)
	}
	if !IsPress("PRESS_LONG") || IsPress("LEVEL") {
		t.Error("IsPress")
	}
	if New(Options{}).o.Interval != 300*time.Millisecond {
		t.Error("defaults")
	}
}
