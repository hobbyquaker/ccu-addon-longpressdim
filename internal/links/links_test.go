package links

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
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

func fake() *system.Fake {
	f := system.NewFake()
	f.AddVirtualRemote("HmIP-RF")
	f.AddDevice("HmIP-RF", "PDT", "HmIP-PDT", "DIMMER_VIRTUAL_RECEIVER", "DIMMER_VIRTUAL_RECEIVER", "DIMMER_VIRTUAL_RECEIVER")
	f.DefaultParamset = hmipDefault
	f.Meta["HmIP-RF.PDT:3"] = system.Object{Name: "Licht"}
	return f
}

func binding() model.Binding {
	return model.Binding{ID: "pdt-3", Target: model.Target{Interface: "HmIP-RF", Address: "PDT:3", Type: "DIMMER_VIRTUAL_RECEIVER", Family: profiles.HmIPDimmer}, Mode: model.TwoKey, Up: "HmIP-RCV-1:50", Down: "HmIP-RCV-1:49",
		Senders: []model.Sender{{Interface: "BidCos-RF", Address: "KEY:1", Role: model.RoleUp}}}
}

func TestPlanAndApplyNew(t *testing.T) {
	f := fake()
	ctx := context.Background()
	objects, _ := f.Objects(ctx, "de")
	p, err := PlanApply(ctx, f, binding(), nil, objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 4 || p.Steps[0].Kind != AddLink || p.Steps[1].Kind != SetMeta || p.Steps[2].Kind != AddLink || p.Steps[3].Kind != SetMeta {
		t.Fatalf("steps %+v", p.Steps)
	}
	if p.Steps[0].Params["LONG_PROFILE_ACTION_TYPE"] != 3 || p.Steps[1].Name == nil || *p.Steps[1].Name != "Licht ▲" || p.Steps[1].Marker.Role != "up" || *p.Steps[3].Name != "Licht ▼" {
		t.Errorf("plan %+v", p.Steps)
	}
	if !p.Writes() {
		t.Error("a plan with links does not write?")
	}
	r := Apply(ctx, f, p)
	if r.Failed != "" || r.Pending {
		t.Fatalf("apply %+v", r)
	}
	// addLink, putParamset (the diff), setMeta per key
	if len(r.Steps) != 6 || r.Steps[1].Step.Kind != PutParamset || r.Steps[1].State != "done" {
		t.Fatalf("results %+v", r.Steps)
	}
	if len(f.CallsOf("addLink")) != 2 || len(f.CallsOf("putParamset")) != 2 {
		t.Errorf("calls %v", f.Calls)
	}
	// only the difference was written
	put := f.CallsOf("putParamset")[0]
	if strings.Contains(put, "LONG_JT_OFF=") || !strings.Contains(put, "LONG_PROFILE_ACTION_TYPE=3") || strings.Contains(put, "LONG_DIM_STEP=") {
		t.Errorf("wrote more than the diff: %s", put)
	}
	ps := f.Paramset("HmIP-RF", "PDT:3", "HmIP-RCV-1:49")
	if c := profiles.Classify(profiles.HmIPDimmer, ps); c.Role != profiles.DownOff {
		t.Errorf("down key classified %+v", c)
	}
	o := f.Meta["HmIP-RF.HmIP-RCV-1:50"]
	if o.Name != "Licht ▲" || o.Marker() == nil || o.Marker().Binding != "pdt-3" {
		t.Errorf("meta %+v", o)
	}
	// the second plan is a no-op: the links exist and carry the profile
	links, _ := system.Links(ctx, f, "HmIP-RF")
	objects, _ = f.Objects(ctx, "de")
	p2, err := PlanApply(ctx, f, binding(), links, objects)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p2.Steps {
		if s.Kind == PutParamset && len(s.Params) != 0 {
			t.Errorf("second plan writes %+v", s)
		}
		if s.Kind == SetMeta && s.Name != nil {
			t.Errorf("second plan renames %+v", s)
		}
	}
	if p2.Writes() {
		t.Error("the no-op plan claims to write")
	}
	// verify agrees
	differs, missing, err := Verify(ctx, f, binding(), links)
	if err != nil || len(differs) != 0 || len(missing) != 0 {
		t.Errorf("verify %v %v %v", differs, missing, err)
	}
	// a change elsewhere shows up
	f.SetParamset("HmIP-RF", "PDT:3", "HmIP-RCV-1:50", map[string]any{"LONG_PROFILE_ACTION_TYPE": 5, "LONG_JT_OFF": 1})
	differs, _, _ = Verify(ctx, f, binding(), links)
	if differs["HmIP-RCV-1:50"]["LONG_PROFILE_ACTION_TYPE"] != 3 {
		t.Errorf("drift not seen: %v", differs)
	}
}

func TestApplyPendingAndFailures(t *testing.T) {
	f := fake()
	f.AddLinkTimeout = true
	ctx := context.Background()
	objects, _ := f.Objects(ctx, "de")
	p, _ := PlanApply(ctx, f, binding(), nil, objects)
	r := Apply(ctx, f, p)
	if r.Failed != "" || !r.Pending || r.Steps[0].State != "pending" {
		t.Fatalf("timeout not taken as pending: %+v", r)
	}
	if len(f.CallsOf("putParamset")) != 2 {
		t.Error("the parameters were not written after the pending addLink")
	}

	f = fake()
	f.Fail["addLink"] = errors.New("rpc -1: Generic error")
	objects, _ = f.Objects(ctx, "de")
	p, _ = PlanApply(ctx, f, binding(), nil, objects)
	r = Apply(ctx, f, p)
	if r.Failed == "" || len(r.Steps) != 1 || r.Steps[0].State != "failed" {
		t.Fatalf("failure not reported: %+v", r)
	}

	f = fake()
	f.Fail["setObject"] = errors.New("403")
	objects, _ = f.Objects(ctx, "de")
	p, _ = PlanApply(ctx, f, binding(), nil, objects)
	r = Apply(ctx, f, p)
	if !strings.Contains(r.Failed, "metadata") {
		t.Fatalf("meta failure: %+v", r)
	}

	// a parameter the device lacks is reported, the rest written
	f = fake()
	f.DefaultParamset = func(iface, receiver string) map[string]json.RawMessage {
		m := hmipDefault(iface, receiver)
		delete(m, "UI_HINT")
		return m
	}
	objects, _ = f.Objects(ctx, "de")
	p, _ = PlanApply(ctx, f, binding(), nil, objects)
	r = Apply(ctx, f, p)
	if r.Failed != "" || len(r.Steps[1].Step.Missing) != 1 || r.Steps[1].Step.Missing[0] != "UI_HINT" {
		t.Fatalf("missing not reported: %+v", r.Steps[1])
	}
}

func TestPlanWarningsAndAdoptedNames(t *testing.T) {
	f := fake()
	ctx := context.Background()
	// the up key is named by the user and marked for another binding; the down key has a
	// foreign link
	m, _ := json.Marshal(system.Marker{Binding: "other", Role: "up"})
	f.Meta["HmIP-RF.HmIP-RCV-1:50"] = system.Object{Name: "Licht DimUp", Meta: map[string]json.RawMessage{system.Namespace: m, "x": json.RawMessage("1")}}
	links := []system.Link{{Sender: "HmIP-RCV-1:49", Receiver: "SW:3"}}
	objects, _ := f.Objects(ctx, "de")
	p, err := PlanApply(ctx, f, binding(), links, objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Warnings) != 3 {
		t.Errorf("warnings %v", p.Warnings)
	}
	if p.Steps[1].Name != nil {
		t.Error("a user's name would be replaced")
	}
	// an invalid binding is refused
	b := binding()
	b.Up = b.Down
	if _, err := PlanApply(ctx, f, b, nil, objects); err == nil {
		t.Error("invalid binding planned")
	}
	// a read failure is an error
	f.Fail["getParamset"] = errors.New("down")
	if _, err := PlanApply(ctx, f, binding(), []system.Link{{Sender: "HmIP-RCV-1:50", Receiver: "PDT:3"}}, objects); err == nil {
		t.Error("read failure ignored")
	}
}

func TestPlanRemoveAndApply(t *testing.T) {
	f := fake()
	ctx := context.Background()
	objects, _ := f.Objects(ctx, "de")
	p, _ := PlanApply(ctx, f, binding(), nil, objects)
	Apply(ctx, f, p)
	links, _ := system.Links(ctx, f, "HmIP-RF")
	objects, _ = f.Objects(ctx, "de")
	rp := PlanRemove(binding(), links, objects, false)
	if len(rp.Steps) != 4 || rp.Steps[0].Kind != RemoveLink || rp.Steps[1].Kind != SetMeta || rp.Steps[1].Marker != nil || rp.Steps[1].Name == nil || *rp.Steps[1].Name != "" {
		t.Fatalf("remove plan %+v", rp.Steps)
	}
	r := Apply(ctx, f, rp)
	if r.Failed != "" {
		t.Fatal(r.Failed)
	}
	links, _ = system.Links(ctx, f, "HmIP-RF")
	if len(links) != 0 {
		t.Error("links remain")
	}
	if o := f.Meta["HmIP-RF.HmIP-RCV-1:50"]; o.Name != "" || o.Marker() != nil {
		t.Errorf("meta remains %+v", o)
	}
	// forget: links stay, a user's name stays, the marker goes
	f = fake()
	objects, _ = f.Objects(ctx, "de")
	p, _ = PlanApply(ctx, f, binding(), nil, objects)
	Apply(ctx, f, p)
	f.Meta["HmIP-RF.HmIP-RCV-1:49"] = system.Object{Name: "Mein Dimmer runter", Meta: f.Meta["HmIP-RF.HmIP-RCV-1:49"].Meta}
	links, _ = system.Links(ctx, f, "HmIP-RF")
	objects, _ = f.Objects(ctx, "de")
	rp = PlanRemove(binding(), links, objects, true)
	if len(rp.Steps) != 2 || rp.Steps[1].Name != nil {
		t.Fatalf("forget plan %+v", rp.Steps)
	}
	Apply(ctx, f, rp)
	links, _ = system.Links(ctx, f, "HmIP-RF")
	if len(links) != 2 || f.Meta["HmIP-RF.HmIP-RCV-1:49"].Name != "Mein Dimmer runter" || f.Meta["HmIP-RF.HmIP-RCV-1:49"].Marker() != nil {
		t.Error("forget changed the links or the name")
	}
	// a missing link is a warning, a foreign marker is kept
	m, _ := json.Marshal(system.Marker{Binding: "other", Role: "down"})
	f.Meta["HmIP-RF.HmIP-RCV-1:49"] = system.Object{Meta: map[string]json.RawMessage{system.Namespace: m}}
	objects, _ = f.Objects(ctx, "de")
	rp = PlanRemove(binding(), nil, objects, false)
	if len(rp.Warnings) != 3 || len(rp.Steps) != 1 {
		t.Errorf("warnings %v steps %+v", rp.Warnings, rp.Steps)
	}
	// a removeLink that times out with the link gone is pending
	f = fake()
	objects, _ = f.Objects(ctx, "de")
	p, _ = PlanApply(ctx, f, binding(), nil, objects)
	Apply(ctx, f, p)
	links, _ = system.Links(ctx, f, "HmIP-RF")
	f.LinksOf["HmIP-RF"] = nil
	f.Fail["removeLink"] = errors.New("rpc -1: Generic error (TIMEOUT)")
	r = Apply(ctx, f, PlanRemove(binding(), links, objects, false))
	if r.Failed != "" || !r.Pending {
		t.Errorf("pending removal %+v", r)
	}
	f.Fail["removeLink"] = errors.New("rpc -1: nope")
	r = Apply(ctx, f, PlanRemove(binding(), links, objects, false))
	if r.Failed == "" {
		t.Error("removal failure ignored")
	}
}

func TestNames(t *testing.T) {
	if Name("Licht", "up") != "Licht ▲" || Name("", "down") != "longpressdim ▼" {
		t.Error("names")
	}
	if LinkName(binding(), "up") != "longpressdim:pdt-3:up" {
		t.Error("link name")
	}
	b := binding()
	b.Mode = model.OneKey
	if Role(b, "up") != profiles.UpToggle || Role(b, "down") != profiles.DownOnly || Role(binding(), "down") != profiles.DownOff {
		t.Error("roles")
	}
	if Pretty(map[string]any{"a": 1}) != `{"a":1}` {
		t.Error("pretty")
	}
}
