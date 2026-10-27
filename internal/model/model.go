// Package model holds the addon's data: a binding is one dimmer or blind actuator, the two
// virtual keys of its interface the addon links to it (up and down), the senders whose presses
// the relay forwards to those keys, and the profile options.
package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
)

// Modes of a binding.
const (
	// TwoKey: the up key switches on (drives up) on a short press and dims up on a long one, the
	// down key switches off and dims down; a sender's role says which key it presses.
	TwoKey = "two-key"
	// OneKey: a short press toggles (the up key's short press), a long press dims up or down in
	// turns - the direction is the addon's, decided per hold.
	OneKey = "one-key"
)

// Sender roles.
const (
	RoleUp   = "up"   // the sender's presses go to the up key
	RoleDown = "down" // ... to the down key
	RoleBoth = "both" // a one-key sender: short toggles, long alternates
)

// Target is the actuator channel the keys are linked to.
type Target struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	// Type is the channel type (DIMMER, HMW_BLIND, DIMMER_VIRTUAL_RECEIVER, ...).
	Type   string          `json:"type"`
	Family profiles.Family `json:"family"`
}

// Sender is a key channel whose presses the relay forwards.
type Sender struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Role      string `json:"role"`
	// Usage is the value usage the addon reported for an HmIP key (nil for the others).
	Usage *Usage `json:"usage,omitempty"`
}

// Same tells whether two senders are the same channel.
func (s Sender) Same(o Sender) bool { return s.Interface == o.Interface && s.Address == o.Address }

// Usage states.
const (
	UsageReported = "reported" // the process took the usage
	UsagePending  = "pending"  // the process keeps it until the device can be reached
	UsageFailed   = "failed"   // the call failed; the key may not report its presses
	UsageReleased = "released"
)

// Usage is what the addon reported to the interface process for a sender: an HmIP key channel
// reports its presses only once a usage is registered for its datapoints
// (reportValueUsage(<channel>, PRESS_SHORT|PRESS_LONG, 1)); on a CCU the logic layer does that
// for the programs that use the key, on openccu-lite this addon does it for its senders.
type Usage struct {
	State  string    `json:"state"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at,omitempty"`
}

// Binding is one managed actuator.
type Binding struct {
	ID     string `json:"id"`
	Target Target `json:"target"`
	Mode   string `json:"mode"`
	// Up and Down are the virtual keys' channel addresses on the target's interface.
	Up      string           `json:"up"`
	Down    string           `json:"down"`
	Senders []Sender         `json:"senders"`
	Options profiles.Options `json:"options"`
	// StepIntervalMS overrides the settings' interval between two relayed long presses; 0 = the
	// settings' value.
	StepIntervalMS int `json:"step_interval_ms,omitempty"`
	// Adopted: the links existed before the addon marked the keys (the Node-RED era); the addon
	// never created them, and "forget" leaves them as they were.
	Adopted bool      `json:"adopted,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// UpRole is the profile role of the up key.
func (b Binding) UpRole() profiles.Role {
	if b.Mode == OneKey {
		return profiles.UpToggle
	}
	return profiles.UpOn
}

// DownRole is the profile role of the down key.
func (b Binding) DownRole() profiles.Role {
	if b.Mode == OneKey {
		return profiles.DownOnly
	}
	return profiles.DownOff
}

// Key answers the key of a direction.
func (b Binding) Key(direction string) string {
	if direction == "up" {
		return b.Up
	}
	return b.Down
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Validate checks the shape (not the system: whether the keys are free is the key inventory's).
func (b Binding) Validate() error {
	if !idRe.MatchString(b.ID) {
		return fmt.Errorf("invalid id %q", b.ID)
	}
	if b.Target.Interface == "" || b.Target.Address == "" {
		return errors.New("target missing")
	}
	if _, ok := profiles.FamilyOf(b.Target.Interface, b.Target.Type); !ok {
		return fmt.Errorf("the channel type %s on %s is not a dimmer or blind the addon links to", b.Target.Type, b.Target.Interface)
	}
	if b.Mode != TwoKey && b.Mode != OneKey {
		return fmt.Errorf("invalid mode %q", b.Mode)
	}
	dev := profiles.KeyDevice(b.Target.Interface)
	for _, k := range []string{b.Up, b.Down} {
		if !strings.HasPrefix(k, dev+":") {
			return fmt.Errorf("key %q is not a virtual key of %s", k, b.Target.Interface)
		}
	}
	if b.Up == b.Down {
		return errors.New("the up and down keys are the same")
	}
	for _, s := range b.Senders {
		if s.Interface == "" || s.Address == "" {
			return errors.New("sender without address")
		}
		if s.Interface == b.Target.Interface {
			return fmt.Errorf("sender %s is on the target's own interface: link it directly", s.Address)
		}
		switch s.Role {
		case RoleUp, RoleDown, RoleBoth:
		default:
			return fmt.Errorf("invalid sender role %q", s.Role)
		}
	}
	if o := b.Options; o.OnLevel < 0 || o.OnLevel > profiles.LastLevel || o.Step < 0 || o.Step > 1 || o.Travel < 0 || o.Travel > 25.5 || o.MinLevel < 0 || o.MaxLevel > 1 || (o.MaxLevel > 0 && o.MinLevel >= o.MaxLevel) {
		return errors.New("options out of range")
	}
	if b.StepIntervalMS != 0 && (b.StepIntervalMS < 100 || b.StepIntervalMS > 2000) {
		return errors.New("step interval out of range (100-2000 ms)")
	}
	return nil
}

// Settings are the addon's own (etc/longpressdim.json).
type Settings struct {
	// StepIntervalMS is the least time between two relayed long presses of one binding; a press
	// that comes sooner is dropped, never queued (faster presses queue in the interface process
	// and the actuator overshoots). 300 by default.
	StepIntervalMS int `json:"step_interval_ms"`
	// HoldTimeoutMS is how long after the last repeat a hold counts as ended when the sender
	// sends no release. 1000 by default.
	HoldTimeoutMS int `json:"hold_timeout_ms"`
	// LogPresses logs every relayed press.
	LogPresses bool `json:"log_presses"`
	// KeepUsages leaves the value usages of HmIP senders registered when a binding is removed
	// (for a key another program - Node-RED, HomeKit - relies on as well: the usage count the
	// process keeps is one number per datapoint, not per client).
	KeepUsages bool `json:"keep_usages"`
}

// DefaultSettings are the values of an unconfigured addon.
func DefaultSettings() Settings {
	return Settings{StepIntervalMS: 300, HoldTimeoutMS: 1000}
}

// Normalize fills zero fields with the defaults and clamps the rest.
func (s Settings) Normalize() Settings {
	d := DefaultSettings()
	if s.StepIntervalMS < 100 || s.StepIntervalMS > 2000 {
		s.StepIntervalMS = d.StepIntervalMS
	}
	if s.HoldTimeoutMS < 300 || s.HoldTimeoutMS > 5000 {
		s.HoldTimeoutMS = d.HoldTimeoutMS
	}
	return s
}
