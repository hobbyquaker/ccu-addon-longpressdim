// Package profiles is the addon's own table of the LINK paramsets a virtual key writes into a
// dimmer or a blind actuator: per family (BidCos-RF, BidCos-Wired, HmIP) and role (the "up" key
// and the "down" key of a binding) the dozen parameters that make a short press switch and a
// long press dim or drive. The values follow the WebUI's profiles for the same links (profile 1
// "on / brighter", 2 "off / darker", 3 "toggle"), read from its easymode files and checked against
// the links on a production system; the table itself is this addon's (MIT), not a copy.
//
// The jump-table enums differ per family: BidCos-RF and HmIP dimmers start with NO_JUMP, the
// wired ones with ONDELAY; the blinds have two more states (REFON, REFOFF). Nothing here is
// portable between families, so every family has its own entry.
package profiles

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Family is the receiver's kind of LINK paramset.
type Family string

const (
	BidCosDimmer Family = "bidcos-dimmer"
	BidCosBlind  Family = "bidcos-blind"
	WiredDimmer  Family = "wired-dimmer"
	WiredBlind   Family = "wired-blind"
	HmIPDimmer   Family = "hmip-dimmer"
	HmIPBlind    Family = "hmip-blind"
)

// Kind is what the family drives.
type Kind string

const (
	Dimmer Kind = "dimmer"
	Blind  Kind = "blind"
)

// Kind answers dimmer or blind.
func (f Family) Kind() Kind {
	if strings.HasSuffix(string(f), "-blind") {
		return Blind
	}
	return Dimmer
}

// Role is what a key of a binding does.
type Role string

const (
	// UpOn is the two-key "up" key: a short press switches on (drives up), a long one dims up.
	UpOn Role = "up-on"
	// DownOff is the two-key "down" key: short off (down), long dim down.
	DownOff Role = "down-off"
	// UpToggle is the one-key "up" key: short toggles, long dims up.
	UpToggle Role = "up-toggle"
	// DownOnly is the one-key "down" key: long dims down, short does nothing.
	DownOnly Role = "down-only"
)

// Direction is the long press's direction of a role.
func (r Role) Direction() string {
	if r == UpOn || r == UpToggle {
		return "up"
	}
	return "down"
}

// FamilyOf answers the family of a receiver channel by its interface and channel type, false
// when the addon does not link to it.
func FamilyOf(iface, channelType string) (Family, bool) {
	switch iface {
	case "BidCos-RF":
		switch channelType {
		case "DIMMER":
			return BidCosDimmer, true
		case "BLIND":
			return BidCosBlind, true
		}
	case "BidCos-Wired":
		switch channelType {
		case "HMW_DIMMER":
			return WiredDimmer, true
		case "HMW_BLIND":
			return WiredBlind, true
		}
	case "HmIP-RF":
		switch channelType {
		case "DIMMER_VIRTUAL_RECEIVER":
			return HmIPDimmer, true
		case "BLIND_VIRTUAL_RECEIVER", "SHUTTER_VIRTUAL_RECEIVER":
			return HmIPBlind, true
		}
	}
	return "", false
}

// KeyDevice answers the interface's virtual remote: the device whose channels 1..50 are the
// virtual keys, "" for an interface without one.
func KeyDevice(iface string) string {
	switch iface {
	case "BidCos-RF":
		return "BidCoS-RF"
	case "BidCos-Wired":
		return "BidCoS-Wir"
	case "HmIP-RF":
		return "HmIP-RCV-1"
	}
	return ""
}

// Keys is how many virtual keys every interface has.
const Keys = 50

// KeyAddress is the address of virtual key n of an interface.
func KeyAddress(iface string, n int) string {
	return fmt.Sprintf("%s:%d", KeyDevice(iface), n)
}

// Options are the per-binding values of a profile.
type Options struct {
	// OnLevel is the dimmer's level of a short "on": 1.0 (full), or 1.005 for "the last level"
	// where the family allows it (BidCos-RF and HmIP; BidCos-Wired clamps to 1.0). 0 = 1.0.
	OnLevel float64 `json:"on_level,omitempty"`
	// Step is the dimmer's step per long press (LONG_DIM_STEP), 0.05 by default.
	Step float64 `json:"step,omitempty"`
	// Travel is the blind's travel time per long press in seconds (LONG_MAX_TIME_FIRST_DIR),
	// 0.5 by default: the relay presses every 0.3 s, so the blind keeps moving while the key is
	// held and stops half a second after the last press.
	Travel float64 `json:"travel,omitempty"`
	// MinLevel and MaxLevel bound the dimming (LONG_DIM_MIN_LEVEL / LONG_DIM_MAX_LEVEL); the
	// defaults 0 and 1 are written as 0 and 0 here (MaxLevel 0 means 1.0).
	MinLevel float64 `json:"min_level,omitempty"`
	MaxLevel float64 `json:"max_level,omitempty"`
}

// Defaults of the options.
const (
	DefaultStep   = 0.05
	DefaultTravel = 0.5
	LastLevel     = 1.005
)

func (o Options) norm(f Family) Options {
	if o.OnLevel <= 0 {
		o.OnLevel = 1
	}
	if o.OnLevel > 1 && f == WiredDimmer {
		o.OnLevel = 1 // no "last level" on the wired dimmers
	}
	if o.Step <= 0 {
		o.Step = DefaultStep
	}
	if o.Travel <= 0 {
		o.Travel = DefaultTravel
	}
	if o.MaxLevel <= 0 {
		o.MaxLevel = 1
	}
	if o.MinLevel < 0 {
		o.MinLevel = 0
	}
	return o
}

// jt is a jump table in the order OFF, ONDELAY, RAMPON, ON, OFFDELAY, RAMPOFF (dimmers) plus
// REFOFF, REFON (blinds).
type jt []int

var jtNames = []string{"OFF", "ONDELAY", "RAMPON", "ON", "OFFDELAY", "RAMPOFF", "REFOFF", "REFON"}

func (t jt) into(p map[string]any, prefix string) {
	for i, v := range t {
		p[prefix+"_JT_"+jtNames[i]] = v
	}
}

// family is one entry of the table.
type family struct {
	kind Kind
	// hmip families write *_PROFILE_ACTION_TYPE, the BidCos ones *_ACTION_TYPE
	actionKey string
	// the action type of a long press per direction, and of a short press that does something
	upAction, downAction, shortAction int
	// the jump tables
	upLong, downLong, shortOn, shortOff, shortToggle jt
	// on-ish and off-ish states, for the classification of a foreign link
	onStates, offStates map[int]bool
	// extra parameters of the long press per direction (the WebUI's profile 2 sets an on time)
	upExtra, downExtra map[string]any
	// extra parameters of every long press (driving mode on the wired blinds)
	longExtra map[string]any
	// uiHint of the pure profiles, "" when the family has no UI_HINT
	uiHintUp, uiHintDown string
}

func set(ids ...int) map[int]bool {
	m := map[int]bool{}
	for _, i := range ids {
		m[i] = true
	}
	return m
}

var table = map[Family]family{
	// enum: 0 NO_JUMP, 1 ONDELAY, 2 RAMPON, 3 ON, 4 OFFDELAY, 5 RAMPOFF, 6 OFF;
	// action: 0 INACTIVE, 1 JUMP_TO_TARGET, 4 UPDIM, 5 DOWNDIM, 6 TOGGLEDIM
	BidCosDimmer: {
		kind: Dimmer, actionKey: "ACTION_TYPE", upAction: 4, downAction: 5, shortAction: 1,
		upLong: jt{1, 2, 3, 2, 2, 2}, downLong: jt{4, 4, 4, 4, 5, 6},
		shortOn: jt{1, 2, 3, 2, 2, 2}, shortOff: jt{4, 4, 4, 4, 5, 6}, shortToggle: jt{1, 2, 3, 4, 5, 6},
		onStates: set(1, 2, 3), offStates: set(4, 5, 6),
		downExtra: map[string]any{"LONG_ON_TIME": 1.0, "LONG_ON_TIME_MODE": 1},
		uiHintUp:  "1", uiHintDown: "2",
	},
	// enum: 0 ONDELAY, 1 RAMPON, 2 ON, 3 OFFDELAY, 4 RAMPOFF, 5 OFF, 6 NO_JUMP
	WiredDimmer: {
		kind: Dimmer, actionKey: "ACTION_TYPE", upAction: 4, downAction: 5, shortAction: 1,
		upLong: jt{0, 1, 2, 1, 1, 1}, downLong: jt{3, 3, 3, 3, 4, 5},
		shortOn: jt{0, 1, 2, 1, 1, 1}, shortOff: jt{3, 3, 3, 3, 4, 5}, shortToggle: jt{0, 1, 2, 3, 4, 5},
		onStates: set(0, 1, 2), offStates: set(3, 4, 5),
		downExtra: map[string]any{"LONG_ON_TIME": 1.0, "LONG_ON_TIME_MODE": 1},
		uiHintUp:  "1", uiHintDown: "2",
	},
	// enum: 0 NOP, 1 ON_DELAY, 2 RAMP_ON, 3 ON, 4 OFF_DELAY, 5 RAMP_OFF, 6 OFF;
	// action: 0 INACTIVE, 1 JUMP_TO_TARGET, 3 UPDIM, 4 DOWNDIM, 5 TOGGLEDIM
	HmIPDimmer: {
		kind: Dimmer, actionKey: "PROFILE_ACTION_TYPE", upAction: 3, downAction: 4, shortAction: 1,
		upLong: jt{1, 2, 3, 2, 2, 2}, downLong: jt{6, 6, 4, 4, 5, 6},
		shortOn: jt{1, 1, 2, 3, 3, 2}, shortOff: jt{6, 6, 4, 4, 5, 6}, shortToggle: jt{1, 2, 3, 4, 5, 6},
		onStates: set(1, 2, 3), offStates: set(4, 5, 6),
		uiHintUp: "1", uiHintDown: "2",
	},
	// enum: 0 NO_JUMP, 1 ONDELAY, 2 REFON, 3 RAMPON, 4 ON, 5 OFFDELAY, 6 REFOFF, 7 RAMPOFF, 8 OFF;
	// action: 0 INACTIVE, 1 JUMP_TO_TARGET (a blind has no dim actions: the jump table drives)
	BidCosBlind: {
		kind: Blind, actionKey: "ACTION_TYPE", upAction: 1, downAction: 1, shortAction: 1,
		upLong: jt{1, 2, 3, 1, 1, 8, 8, 3}, downLong: jt{5, 5, 4, 5, 6, 7, 7, 4},
		shortOn: jt{1, 2, 3, 1, 1, 8, 8, 3}, shortOff: jt{5, 5, 4, 5, 6, 7, 7, 4}, shortToggle: jt{1, 2, 4, 5, 6, 8, 8, 4},
		onStates: set(1, 2, 3, 4), offStates: set(5, 6, 7, 8),
		uiHintUp: "1", uiHintDown: "2",
	},
	// enum: 0 ONDELAY, 1 REFON, 2 RAMPON, 3 ON, 4 OFFDELAY, 5 REFOFF, 6 RAMPOFF, 7 OFF, 8 NO_JUMP;
	// action: 0 INACTIVE, 1 ACTIVE
	WiredBlind: {
		kind: Blind, actionKey: "ACTION_TYPE", upAction: 1, downAction: 1, shortAction: 1,
		upLong: jt{0, 1, 2, 0, 0, 7, 7, 2}, downLong: jt{4, 4, 3, 4, 5, 6, 6, 3},
		shortOn: jt{0, 1, 2, 0, 0, 7, 7, 2}, shortOff: jt{4, 4, 3, 4, 5, 6, 6, 3}, shortToggle: jt{0, 1, 3, 4, 5, 7, 7, 3},
		onStates: set(0, 1, 2, 3), offStates: set(4, 5, 6, 7),
		longExtra: map[string]any{"LONG_DRIVING_MODE": 3},
		uiHintUp:  "1", uiHintDown: "2",
	},
	// enum: 0 NOP, 1 ON_DELAY, 2 RAMP_ON, 3 ON, 4 OFF_DELAY, 5 RAMP_OFF, 6 OFF, 7 REFON, 8 REFOFF;
	// action: 0 INACTIVE, 1 JUMP_TO_TARGET
	HmIPBlind: {
		kind: Blind, actionKey: "PROFILE_ACTION_TYPE", upAction: 1, downAction: 1, shortAction: 1,
		upLong: jt{1, 7, 2, 1, 1, 6, 6, 2}, downLong: jt{4, 4, 3, 4, 8, 5, 5, 3},
		shortOn: jt{1, 1, 3, 1, 3, 6, 8, 7}, shortOff: jt{4, 6, 3, 4, 4, 6, 8, 7}, shortToggle: jt{1, 1, 3, 4, 4, 6, 7, 8},
		onStates: set(1, 2, 3, 7), offStates: set(4, 5, 6, 8),
		uiHintUp: "1", uiHintDown: "2",
	},
}

// Families lists the table's families, sorted.
func Families() []Family {
	out := make([]Family, 0, len(table))
	for f := range table {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Params answers the LINK parameters a key of the role writes into a receiver of the family:
// the receiver's paramset `getParamset(<receiver>, <key>)` is compared with them (Diff) and only
// what differs is written. Integers are int, floats float64, flags bool, UI_HINT a string.
func Params(f Family, r Role, o Options) (map[string]any, error) {
	t, ok := table[f]
	if !ok {
		return nil, fmt.Errorf("profiles: unknown family %q", f)
	}
	o = o.norm(f)
	p := map[string]any{"LONG_MULTIEXECUTE": true}
	long, short := "LONG_", "SHORT_"
	switch r.Direction() {
	case "up":
		p[long+t.actionKey] = t.upAction
		t.upLong.into(p, "LONG")
		for k, v := range t.upExtra {
			p[k] = v
		}
	default:
		p[long+t.actionKey] = t.downAction
		t.downLong.into(p, "LONG")
		for k, v := range t.downExtra {
			p[k] = v
		}
	}
	for k, v := range t.longExtra {
		p[k] = v
	}
	switch r {
	case UpOn:
		p[short+t.actionKey] = t.shortAction
		t.shortOn.into(p, "SHORT")
	case DownOff:
		p[short+t.actionKey] = t.shortAction
		t.shortOff.into(p, "SHORT")
	case UpToggle:
		p[short+t.actionKey] = t.shortAction
		t.shortToggle.into(p, "SHORT")
	case DownOnly:
		p[short+t.actionKey] = 0
	default:
		return nil, fmt.Errorf("profiles: unknown role %q", r)
	}
	switch t.kind {
	case Dimmer:
		p["LONG_DIM_STEP"] = o.Step
		p["LONG_DIM_MIN_LEVEL"] = o.MinLevel
		p["LONG_DIM_MAX_LEVEL"] = o.MaxLevel
		if r == UpOn || r == UpToggle {
			p["SHORT_ON_LEVEL"] = o.OnLevel
		}
	case Blind:
		p["LONG_MAX_TIME_FIRST_DIR"] = o.Travel
		if r != DownOnly {
			p["SHORT_MAX_TIME_FIRST_DIR"] = 25.5
		}
	}
	switch {
	case r == UpOn && t.uiHintUp != "":
		p["UI_HINT"] = t.uiHintUp
	case r == DownOff && t.uiHintDown != "":
		p["UI_HINT"] = t.uiHintDown
	case t.uiHintUp != "":
		p["UI_HINT"] = "0"
	}
	return p, nil
}

// Diff answers the parameters of want whose value differs from the receiver's current paramset,
// and the names of the parameters the paramset does not have at all (never written: a device
// without them would refuse the whole write). A number is compared with a tolerance of 1e-6,
// booleans also against 0/1, strings exactly.
func Diff(want map[string]any, current map[string]json.RawMessage) (diff map[string]any, missing []string) {
	diff = map[string]any{}
	for k, v := range want {
		raw, ok := current[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		if !Equal(v, raw) {
			diff[k] = v
		}
	}
	sort.Strings(missing)
	return diff, missing
}

// Equal tells whether a wanted value and a paramset's raw JSON value are the same.
func Equal(want any, raw json.RawMessage) bool {
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return false
	}
	switch w := want.(type) {
	case bool:
		switch c := cur.(type) {
		case bool:
			return c == w
		case float64:
			return (c != 0) == w
		}
		return false
	case int:
		return numEqual(float64(w), cur)
	case float64:
		return numEqual(w, cur)
	case string:
		c, ok := cur.(string)
		return ok && c == w
	}
	return false
}

func numEqual(w float64, cur any) bool {
	switch c := cur.(type) {
	case float64:
		return math.Abs(c-w) < 1e-6
	case bool:
		if c {
			return w == 1
		}
		return w == 0
	}
	return false
}

// Classification is what a foreign link's paramset says about the key.
type Classification struct {
	// Direction of the long press: up, down, toggle (the actuator alternates), none, other.
	Long string `json:"long"`
	// Short press: on, off, toggle, none, other.
	Short string `json:"short"`
	// Role is the addon's role that matches, "" when none does.
	Role Role `json:"role,omitempty"`
	// Step is the dimmer's LONG_DIM_STEP or the blind's LONG_MAX_TIME_FIRST_DIR, 0 when absent.
	Step float64 `json:"step,omitempty"`
	// OnLevel is the dimmer's SHORT_ON_LEVEL, 0 when absent.
	OnLevel float64 `json:"on_level,omitempty"`
}

// Classify reads a receiver's LINK paramset of a key and says what the key does. The direction
// of a dimmer's long press is its action type; a blind's and every short press are read from
// where the jump table leads from OFF and from ON.
func Classify(f Family, ps map[string]json.RawMessage) Classification {
	t, ok := table[f]
	c := Classification{Long: "other", Short: "other"}
	if !ok {
		return c
	}
	num := func(k string) (float64, bool) {
		raw, ok := ps[k]
		if !ok {
			return 0, false
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return 0, false
		}
		switch x := v.(type) {
		case float64:
			return x, true
		case bool:
			if x {
				return 1, true
			}
			return 0, true
		}
		return 0, false
	}
	leads := func(prefix string) string {
		off, ok1 := num(prefix + "_JT_OFF")
		on, ok2 := num(prefix + "_JT_ON")
		if !ok1 || !ok2 {
			return "other"
		}
		fromOff, fromOn := int(off), int(on)
		switch {
		case t.onStates[fromOff] && t.onStates[fromOn]:
			return "on"
		case t.offStates[fromOff] && t.offStates[fromOn]:
			return "off"
		case t.onStates[fromOff] && t.offStates[fromOn]:
			return "toggle"
		}
		return "other"
	}
	la, _ := num("LONG_" + t.actionKey)
	sa, _ := num("SHORT_" + t.actionKey)
	switch {
	case int(la) == 0:
		c.Long = "none"
	case t.kind == Dimmer && int(la) == t.upAction:
		c.Long = "up"
	case t.kind == Dimmer && int(la) == t.downAction:
		c.Long = "down"
	case t.kind == Dimmer && int(la) == 1:
		c.Long = "none" // a jump: on, off or toggle, no dimming
	case t.kind == Dimmer:
		c.Long = "toggle" // TOGGLEDIM and its counter variants
	default:
		switch leads("LONG") {
		case "on":
			c.Long = "up"
		case "off":
			c.Long = "down"
		case "toggle":
			c.Long = "toggle"
		}
	}
	if int(sa) == 0 {
		c.Short = "none"
	} else if int(sa) == t.shortAction {
		c.Short = leads("SHORT")
	}
	switch {
	case c.Long == "up" && c.Short == "on":
		c.Role = UpOn
	case c.Long == "up" && c.Short == "toggle":
		c.Role = UpToggle
	case c.Long == "down" && c.Short == "off":
		c.Role = DownOff
	case c.Long == "down" && c.Short == "none":
		c.Role = DownOnly
	}
	if t.kind == Dimmer {
		c.Step, _ = num("LONG_DIM_STEP")
		c.OnLevel, _ = num("SHORT_ON_LEVEL")
	} else {
		c.Step, _ = num("LONG_MAX_TIME_FIRST_DIR")
	}
	return c
}
