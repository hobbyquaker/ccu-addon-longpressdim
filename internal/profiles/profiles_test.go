package profiles

import (
	"encoding/json"
	"fmt"
	"testing"
)

func raw(m map[string]any) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range m {
		b, _ := json.Marshal(v)
		out[k] = b
	}
	return out
}

// hmipDefault is the LINK paramset hmipserver gave a fresh HmIP-RCV-50 -> HmIP-PDT link on the
// lab box (the WebUI's profile 3: toggle / dim alternating), the measured starting point.
func hmipDefault() map[string]json.RawMessage {
	return raw(map[string]any{
		"LONG_PROFILE_ACTION_TYPE": 5, "SHORT_PROFILE_ACTION_TYPE": 1, "LONG_MULTIEXECUTE": true,
		"LONG_JT_OFF": 1, "LONG_JT_ONDELAY": 2, "LONG_JT_RAMPON": 3, "LONG_JT_ON": 4, "LONG_JT_OFFDELAY": 5, "LONG_JT_RAMPOFF": 6,
		"SHORT_JT_OFF": 1, "SHORT_JT_ONDELAY": 2, "SHORT_JT_RAMPON": 3, "SHORT_JT_ON": 4, "SHORT_JT_OFFDELAY": 5, "SHORT_JT_RAMPOFF": 6,
		"LONG_DIM_STEP": 0.05, "LONG_DIM_MIN_LEVEL": 0.0, "LONG_DIM_MAX_LEVEL": 1.0, "SHORT_ON_LEVEL": 1.0, "UI_HINT": "3",
	})
}

// wiredDown and wiredUp are the shape of the DimDown / DimUp links of an HMW-LC-Dim1L-DR on a
// production system (values only, the WebUI's profiles 2 and 1 with a 3 % step).
func wiredDown() map[string]json.RawMessage {
	return raw(map[string]any{
		"LONG_ACTION_TYPE": 5, "SHORT_ACTION_TYPE": 1, "LONG_MULTIEXECUTE": true, "LONG_DIM_STEP": 0.03,
		"LONG_JT_OFF": 3, "LONG_JT_ONDELAY": 3, "LONG_JT_RAMPON": 3, "LONG_JT_ON": 3, "LONG_JT_OFFDELAY": 4, "LONG_JT_RAMPOFF": 5,
		"SHORT_JT_OFF": 3, "SHORT_JT_ONDELAY": 3, "SHORT_JT_RAMPON": 3, "SHORT_JT_ON": 3, "SHORT_JT_OFFDELAY": 4, "SHORT_JT_RAMPOFF": 5,
		"LONG_ON_TIME": 1.0, "LONG_ON_TIME_MODE": 1, "LONG_DIM_MIN_LEVEL": 0.0, "LONG_DIM_MAX_LEVEL": 1.0, "SHORT_ON_LEVEL": 1.0, "UI_HINT": "",
	})
}

func wiredUp() map[string]json.RawMessage {
	return raw(map[string]any{
		"LONG_ACTION_TYPE": 4, "SHORT_ACTION_TYPE": 1, "LONG_MULTIEXECUTE": true, "LONG_DIM_STEP": 0.03,
		"LONG_JT_OFF": 0, "LONG_JT_ONDELAY": 1, "LONG_JT_RAMPON": 2, "LONG_JT_ON": 1, "LONG_JT_OFFDELAY": 1, "LONG_JT_RAMPOFF": 1,
		"SHORT_JT_OFF": 0, "SHORT_JT_ONDELAY": 1, "SHORT_JT_RAMPON": 2, "SHORT_JT_ON": 1, "SHORT_JT_OFFDELAY": 1, "SHORT_JT_RAMPOFF": 1,
		"LONG_ON_TIME": 16383000.0, "LONG_ON_TIME_MODE": 0, "LONG_DIM_MIN_LEVEL": 0.0, "LONG_DIM_MAX_LEVEL": 1.0, "SHORT_ON_LEVEL": 1.0, "UI_HINT": "",
	})
}

func TestParamsEveryFamilyAndRole(t *testing.T) {
	for _, f := range Families() {
		for _, r := range []Role{UpOn, DownOff, UpToggle, DownOnly} {
			p, err := Params(f, r, Options{})
			if err != nil {
				t.Fatalf("%s %s: %v", f, r, err)
			}
			if p["LONG_MULTIEXECUTE"] != true {
				t.Errorf("%s %s: LONG_MULTIEXECUTE missing", f, r)
			}
			n := 6
			if f.Kind() == Blind {
				n = 8
			}
			for _, name := range jtNames[:n] {
				if _, ok := p["LONG_JT_"+name]; !ok {
					t.Errorf("%s %s: LONG_JT_%s missing", f, r, name)
				}
				if _, ok := p["SHORT_JT_"+name]; ok && r == DownOnly {
					t.Errorf("%s %s: a down-only key writes no short jump table", f, r)
				}
				if _, ok := p["SHORT_JT_"+name]; !ok && r != DownOnly {
					t.Errorf("%s %s: SHORT_JT_%s missing", f, r, name)
				}
			}
			if f.Kind() == Dimmer {
				if p["LONG_DIM_STEP"] != DefaultStep {
					t.Errorf("%s %s: step %v", f, r, p["LONG_DIM_STEP"])
				}
			} else if p["LONG_MAX_TIME_FIRST_DIR"] != DefaultTravel {
				t.Errorf("%s %s: travel %v", f, r, p["LONG_MAX_TIME_FIRST_DIR"])
			}
			// the table round-trips through the classifier
			c := Classify(f, raw(p))
			if c.Role != r {
				t.Errorf("%s %s: classified as %q (long %s, short %s)", f, r, c.Role, c.Long, c.Short)
			}
		}
	}
	if _, err := Params("nope", UpOn, Options{}); err == nil {
		t.Error("unknown family accepted")
	}
	if _, err := Params(BidCosDimmer, "x", Options{}); err == nil {
		t.Error("unknown role accepted")
	}
}

func TestParamsValues(t *testing.T) {
	cases := []struct {
		f    Family
		r    Role
		o    Options
		want map[string]any
	}{
		{BidCosDimmer, UpOn, Options{}, map[string]any{"LONG_ACTION_TYPE": 4, "SHORT_ACTION_TYPE": 1, "LONG_JT_OFF": 1, "LONG_JT_RAMPON": 3, "LONG_JT_ON": 2, "SHORT_ON_LEVEL": 1.0, "UI_HINT": "1"}},
		{BidCosDimmer, DownOff, Options{Step: 0.03}, map[string]any{"LONG_ACTION_TYPE": 5, "LONG_JT_OFF": 4, "LONG_JT_RAMPOFF": 6, "LONG_ON_TIME": 1.0, "LONG_ON_TIME_MODE": 1, "LONG_DIM_STEP": 0.03, "UI_HINT": "2"}},
		{BidCosDimmer, UpToggle, Options{OnLevel: LastLevel}, map[string]any{"LONG_ACTION_TYPE": 4, "SHORT_JT_OFF": 1, "SHORT_JT_ON": 4, "SHORT_ON_LEVEL": LastLevel, "UI_HINT": "0"}},
		{BidCosDimmer, DownOnly, Options{}, map[string]any{"LONG_ACTION_TYPE": 5, "SHORT_ACTION_TYPE": 0, "UI_HINT": "0"}},
		{WiredDimmer, UpOn, Options{OnLevel: LastLevel}, map[string]any{"LONG_ACTION_TYPE": 4, "LONG_JT_OFF": 0, "LONG_JT_RAMPON": 2, "SHORT_ON_LEVEL": 1.0}},
		{WiredDimmer, DownOff, Options{}, map[string]any{"LONG_ACTION_TYPE": 5, "LONG_JT_OFF": 3, "LONG_JT_OFFDELAY": 4, "LONG_JT_RAMPOFF": 5}},
		{HmIPDimmer, UpOn, Options{}, map[string]any{"LONG_PROFILE_ACTION_TYPE": 3, "SHORT_PROFILE_ACTION_TYPE": 1, "LONG_JT_OFF": 1, "LONG_JT_ON": 2, "SHORT_JT_ON": 3, "SHORT_JT_OFFDELAY": 3}},
		{HmIPDimmer, DownOff, Options{}, map[string]any{"LONG_PROFILE_ACTION_TYPE": 4, "LONG_JT_OFF": 6, "LONG_JT_RAMPON": 4, "SHORT_JT_OFF": 6}},
		{HmIPDimmer, DownOnly, Options{}, map[string]any{"LONG_PROFILE_ACTION_TYPE": 4, "SHORT_PROFILE_ACTION_TYPE": 0}},
		{BidCosBlind, UpOn, Options{Travel: 0.8}, map[string]any{"LONG_ACTION_TYPE": 1, "LONG_JT_OFF": 1, "LONG_JT_REFON": 3, "LONG_JT_RAMPOFF": 8, "LONG_MAX_TIME_FIRST_DIR": 0.8, "SHORT_MAX_TIME_FIRST_DIR": 25.5}},
		{BidCosBlind, DownOff, Options{}, map[string]any{"LONG_JT_OFF": 5, "LONG_JT_RAMPON": 4, "LONG_JT_REFOFF": 7, "LONG_MAX_TIME_FIRST_DIR": 0.5}},
		{WiredBlind, UpOn, Options{}, map[string]any{"LONG_ACTION_TYPE": 1, "LONG_DRIVING_MODE": 3, "LONG_JT_OFF": 0, "LONG_JT_RAMPOFF": 7}},
		{WiredBlind, DownOnly, Options{}, map[string]any{"SHORT_ACTION_TYPE": 0, "LONG_JT_OFF": 4, "LONG_JT_RAMPOFF": 6}},
		{HmIPBlind, UpOn, Options{}, map[string]any{"LONG_PROFILE_ACTION_TYPE": 1, "LONG_JT_ONDELAY": 7, "LONG_JT_RAMPOFF": 6, "SHORT_JT_OFF": 1}},
		{HmIPBlind, DownOff, Options{}, map[string]any{"LONG_JT_OFF": 4, "LONG_JT_OFFDELAY": 8, "SHORT_JT_OFF": 4, "SHORT_JT_REFOFF": 8}},
	}
	for _, c := range cases {
		p, err := Params(c.f, c.r, c.o)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range c.want {
			if p[k] != v {
				t.Errorf("%s %s: %s = %v, want %v", c.f, c.r, k, p[k], v)
			}
		}
	}
}

func TestDiffAgainstMeasuredDefault(t *testing.T) {
	// the lab: from hmipserver's default link a U-on key needs the action type and the jumps
	// out of the toggle, the D-off key the same the other way round; a second run writes nothing
	p, _ := Params(HmIPDimmer, UpOn, Options{})
	diff, missing := Diff(p, hmipDefault())
	for _, k := range []string{"LONG_PROFILE_ACTION_TYPE", "LONG_JT_ON", "LONG_JT_OFFDELAY", "LONG_JT_RAMPOFF", "SHORT_JT_ON", "SHORT_JT_OFFDELAY", "SHORT_JT_RAMPOFF", "UI_HINT"} {
		if _, ok := diff[k]; !ok {
			t.Errorf("U-on diff lacks %s: %v", k, diff)
		}
	}
	for _, k := range []string{"LONG_JT_OFF", "LONG_JT_RAMPON", "LONG_MULTIEXECUTE", "LONG_DIM_STEP", "SHORT_ON_LEVEL"} {
		if _, ok := diff[k]; ok {
			t.Errorf("U-on diff rewrites the unchanged %s", k)
		}
	}
	if len(missing) != 0 {
		t.Errorf("missing %v", missing)
	}
	cur := hmipDefault()
	for k, v := range diff {
		b, _ := json.Marshal(v)
		cur[k] = b
	}
	if again, _ := Diff(p, cur); len(again) != 0 {
		t.Errorf("after the write the diff is %v", again)
	}
	// a parameter the device lacks is reported, never written
	p["NOT_THERE"] = 1
	_, missing = Diff(p, hmipDefault())
	if len(missing) != 1 || missing[0] != "NOT_THERE" {
		t.Errorf("missing = %v", missing)
	}
}

func TestDiffWiredProductionStep(t *testing.T) {
	// the production links carry the WebUI's profile with a 3 % step: with that step nothing
	// differs; with the default step only LONG_DIM_STEP and the UI_HINT do
	p, _ := Params(WiredDimmer, DownOff, Options{Step: 0.03})
	diff, _ := Diff(p, wiredDown())
	if len(diff) != 1 || diff["UI_HINT"] != "2" {
		t.Errorf("down diff = %v", diff)
	}
	p, _ = Params(WiredDimmer, UpOn, Options{})
	diff, _ = Diff(p, wiredUp())
	if len(diff) != 2 || diff["LONG_DIM_STEP"] != DefaultStep || diff["UI_HINT"] != "1" {
		t.Errorf("up diff = %v", diff)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		f     Family
		ps    map[string]json.RawMessage
		long  string
		short string
		role  Role
		step  float64
	}{
		{"hmip default toggle", HmIPDimmer, hmipDefault(), "toggle", "toggle", "", 0.05},
		{"wired DimDown", WiredDimmer, wiredDown(), "down", "off", DownOff, 0.03},
		{"wired DimUp", WiredDimmer, wiredUp(), "up", "on", UpOn, 0.03},
		{"bidcos toggle key with long down", BidCosDimmer, raw(map[string]any{
			"LONG_ACTION_TYPE": 5, "SHORT_ACTION_TYPE": 1, "LONG_JT_OFF": 4, "LONG_JT_ON": 4, "SHORT_JT_OFF": 1, "SHORT_JT_ON": 4, "LONG_DIM_STEP": 0.05,
		}), "down", "toggle", "", 0.05},
		{"bidcos jump (no dim)", BidCosDimmer, raw(map[string]any{
			"LONG_ACTION_TYPE": 1, "SHORT_ACTION_TYPE": 1, "LONG_JT_OFF": 1, "LONG_JT_ON": 2, "SHORT_JT_OFF": 1, "SHORT_JT_ON": 2,
		}), "none", "on", "", 0},
		{"wired blind up", WiredBlind, raw(map[string]any{
			"LONG_ACTION_TYPE": 1, "SHORT_ACTION_TYPE": 1, "LONG_JT_OFF": 0, "LONG_JT_ON": 0, "SHORT_JT_OFF": 0, "SHORT_JT_ON": 0, "LONG_MAX_TIME_FIRST_DIR": 0.5,
		}), "up", "on", UpOn, 0.5},
		{"bidcos blind toggle", BidCosBlind, raw(map[string]any{
			"LONG_ACTION_TYPE": 1, "SHORT_ACTION_TYPE": 1, "LONG_JT_OFF": 1, "LONG_JT_ON": 5, "SHORT_JT_OFF": 1, "SHORT_JT_ON": 5,
		}), "toggle", "toggle", "", 0},
		{"inactive", HmIPDimmer, raw(map[string]any{"LONG_PROFILE_ACTION_TYPE": 0, "SHORT_PROFILE_ACTION_TYPE": 0}), "none", "none", "", 0},
		{"empty", HmIPDimmer, map[string]json.RawMessage{}, "none", "none", "", 0},
		{"unknown family", "x", hmipDefault(), "other", "other", "", 0},
	}
	for _, c := range cases {
		got := Classify(c.f, c.ps)
		if got.Long != c.long || got.Short != c.short || got.Role != c.role || got.Step != c.step {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func TestEqual(t *testing.T) {
	cases := []struct {
		want any
		raw  string
		eq   bool
	}{
		{true, "true", true}, {true, "1", true}, {false, "0", true}, {true, "0", false}, {true, `"x"`, false},
		{1, "1", true}, {1, "1.0", true}, {1, "true", true}, {0, "false", true}, {2, "1", false}, {1, `"1"`, false},
		{0.05, "0.050000", true}, {0.05, "0.03", false},
		{"1", `"1"`, true}, {"1", "1", false}, {"1", `"2"`, false},
		{1, "nope", false}, {struct{}{}, "1", false},
	}
	for _, c := range cases {
		if got := Equal(c.want, json.RawMessage(c.raw)); got != c.eq {
			t.Errorf("Equal(%v, %s) = %v", c.want, c.raw, got)
		}
	}
}

func TestFamilyOfAndKeys(t *testing.T) {
	cases := []struct {
		iface, typ string
		f          Family
		ok         bool
	}{
		{"BidCos-RF", "DIMMER", BidCosDimmer, true}, {"BidCos-RF", "BLIND", BidCosBlind, true}, {"BidCos-RF", "SWITCH", "", false},
		{"BidCos-Wired", "HMW_DIMMER", WiredDimmer, true}, {"BidCos-Wired", "HMW_BLIND", WiredBlind, true}, {"BidCos-Wired", "HMW_SWITCH", "", false},
		{"HmIP-RF", "DIMMER_VIRTUAL_RECEIVER", HmIPDimmer, true}, {"HmIP-RF", "BLIND_VIRTUAL_RECEIVER", HmIPBlind, true},
		{"HmIP-RF", "SHUTTER_VIRTUAL_RECEIVER", HmIPBlind, true}, {"HmIP-RF", "SWITCH_VIRTUAL_RECEIVER", "", false},
		{"VirtualDevices", "DIMMER", "", false},
	}
	for _, c := range cases {
		f, ok := FamilyOf(c.iface, c.typ)
		if f != c.f || ok != c.ok {
			t.Errorf("FamilyOf(%s, %s) = %s %v", c.iface, c.typ, f, ok)
		}
	}
	if BidCosBlind.Kind() != Blind || HmIPDimmer.Kind() != Dimmer {
		t.Error("kind")
	}
	for iface, want := range map[string]string{"BidCos-RF": "BidCoS-RF:48", "BidCos-Wired": "BidCoS-Wir:48", "HmIP-RF": "HmIP-RCV-1:48"} {
		if got := KeyAddress(iface, 48); got != want {
			t.Errorf("KeyAddress(%s) = %s", iface, got)
		}
	}
	if KeyDevice("VirtualDevices") != "" {
		t.Error("VirtualDevices has no virtual remote")
	}
	if UpOn.Direction() != "up" || DownOnly.Direction() != "down" {
		t.Error("direction")
	}
}

func ExampleParams() {
	p, _ := Params(WiredDimmer, DownOff, Options{Step: 0.03})
	fmt.Println(p["LONG_ACTION_TYPE"], p["LONG_DIM_STEP"], p["LONG_JT_OFF"], p["SHORT_ACTION_TYPE"])
	// Output: 5 0.03 3 1
}
