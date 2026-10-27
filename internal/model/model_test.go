package model

import (
	"testing"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
)

func valid() Binding {
	return Binding{ID: "pdt-3", Target: Target{Interface: "HmIP-RF", Address: "PDT:3", Type: "DIMMER_VIRTUAL_RECEIVER", Family: profiles.HmIPDimmer}, Mode: TwoKey, Up: "HmIP-RCV-1:50", Down: "HmIP-RCV-1:49",
		Senders: []Sender{{Interface: "BidCos-RF", Address: "RC:1", Role: RoleUp}}}
}

func TestValidate(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		f    func(b *Binding)
	}{
		{"id", func(b *Binding) { b.ID = "Bad Id" }},
		{"no target", func(b *Binding) { b.Target.Address = "" }},
		{"not a dimmer", func(b *Binding) { b.Target.Type = "SWITCH_VIRTUAL_RECEIVER" }},
		{"mode", func(b *Binding) { b.Mode = "three-key" }},
		{"foreign key", func(b *Binding) { b.Up = "BidCoS-RF:50" }},
		{"same keys", func(b *Binding) { b.Down = b.Up }},
		{"sender without address", func(b *Binding) { b.Senders[0].Address = "" }},
		{"sender on the target's interface", func(b *Binding) { b.Senders[0].Interface = "HmIP-RF" }},
		{"sender role", func(b *Binding) { b.Senders[0].Role = "sideways" }},
		{"on level", func(b *Binding) { b.Options.OnLevel = 2 }},
		{"step", func(b *Binding) { b.Options.Step = 1.5 }},
		{"travel", func(b *Binding) { b.Options.Travel = 30 }},
		{"levels", func(b *Binding) { b.Options.MinLevel = 0.8; b.Options.MaxLevel = 0.5 }},
		{"interval", func(b *Binding) { b.StepIntervalMS = 50 }},
	}
	for _, c := range cases {
		b := valid()
		c.f(&b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s accepted", c.name)
		}
	}
}

func TestRolesAndKeys(t *testing.T) {
	b := valid()
	if b.UpRole() != profiles.UpOn || b.DownRole() != profiles.DownOff || b.Key("up") != b.Up || b.Key("down") != b.Down {
		t.Error("two-key roles")
	}
	b.Mode = OneKey
	if b.UpRole() != profiles.UpToggle || b.DownRole() != profiles.DownOnly {
		t.Error("one-key roles")
	}
}

func TestSettingsNormalize(t *testing.T) {
	s := Settings{StepIntervalMS: 5000, HoldTimeoutMS: 0, LogPresses: true}.Normalize()
	if s.StepIntervalMS != 300 || s.HoldTimeoutMS != 1000 || !s.LogPresses {
		t.Errorf("%+v", s)
	}
	s = Settings{StepIntervalMS: 450, HoldTimeoutMS: 2000}.Normalize()
	if s.StepIntervalMS != 450 || s.HoldTimeoutMS != 2000 {
		t.Errorf("%+v", s)
	}
}
