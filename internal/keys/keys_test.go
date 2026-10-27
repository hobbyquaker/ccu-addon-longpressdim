package keys

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/system"
)

func TestInventoryFreeAndPick(t *testing.T) {
	links := []system.Link{
		{Sender: "HmIP-RCV-1:1", Receiver: "PDT:3"},
		{Sender: "WRC:1", Receiver: "HmIP-RCV-1:2"}, // a key as receiver counts too
		{Sender: "HmIP-RCV-1:50", Receiver: "PDT:3"},
	}
	marker, _ := json.Marshal(system.Marker{Binding: "pdt", Role: "up"})
	objects := system.Objects{
		"HmIP-RF.HmIP-RCV-1:3":   {Name: "Licht DimUp"},
		"HmIP-RF.HmIP-RCV-1:49":  {Meta: map[string]json.RawMessage{system.Namespace: marker}},
		"HmIP-RF.HmIP-RCV-1:48":  {Meta: map[string]json.RawMessage{"other": json.RawMessage(`{}`)}},
		"BidCos-RF.BidCoS-RF:47": {Name: "not this interface"},
	}
	inv := Inventory("HmIP-RF", links, objects)
	if len(inv) != 50 || inv[0].N != 1 || inv[49].Address != "HmIP-RCV-1:50" {
		t.Fatalf("inventory %d", len(inv))
	}
	want := map[int]bool{1: false, 2: false, 3: false, 48: false, 49: false, 50: false, 47: true, 4: true, 5: true, 6: true}
	for n, free := range want {
		if inv[n-1].Free != free {
			t.Errorf("key %d free = %v: %+v", n, inv[n-1].Free, inv[n-1])
		}
	}
	if inv[48].Marker == nil || inv[48].Marker.Binding != "pdt" || !inv[47].Foreign || len(inv[1].Links) != 1 {
		t.Error("marker, foreign or links wrong")
	}
	if f := Free(inv); f[0] != 47 || f[1] != 46 || len(f) != 44 {
		t.Errorf("free = %v", f)
	}
	picked, err := Pick(inv, 2)
	if err != nil || picked[0] != "HmIP-RCV-1:47" || picked[1] != "HmIP-RCV-1:46" {
		t.Errorf("pick %v %v", picked, err)
	}
	if _, err := Pick(inv, 45); err == nil {
		t.Error("picked more than free")
	}
	if k, ok := Find(inv, "HmIP-RCV-1:3"); !ok || k.Name != "Licht DimUp" {
		t.Error("find")
	}
	if _, ok := Find(inv, "x"); ok {
		t.Error("found x")
	}
	if Inventory("VirtualDevices", nil, nil) != nil {
		t.Error("VirtualDevices has keys")
	}
}

func devs() []litesource.DeviceDescription {
	return []litesource.DeviceDescription{
		{Address: "PDT", Type: "HmIP-PDT"},
		{Address: "PDT:0", Type: "MAINTENANCE", Parent: "PDT"},
		{Address: "PDT:3", Type: "DIMMER_VIRTUAL_RECEIVER", Parent: "PDT"},
		{Address: "BROLL", Type: "HmIP-BROLL"},
		{Address: "BROLL:4", Type: "SHUTTER_VIRTUAL_RECEIVER", Parent: "BROLL"},
		{Address: "WRC", Type: "HmIP-WRC2"},
		{Address: "WRC:1", Type: "KEY_TRANSCEIVER", Parent: "WRC"},
		{Address: "WRC:2", Type: "KEY_TRANSCEIVER", Parent: "WRC"},
		{Address: "HmIP-RCV-1", Type: "HmIP-RCV-50"},
		{Address: "HmIP-RCV-1:1", Type: "KEY_TRANSCEIVER", Parent: "HmIP-RCV-1"},
		{Address: "SW", Type: "HmIP-PS"},
		{Address: "SW:3", Type: "SWITCH_VIRTUAL_RECEIVER", Parent: "SW"},
	}
}

func TestTargetsAndSenders(t *testing.T) {
	objects := system.Objects{"HmIP-RF.PDT:3": {Name: "Licht", Rooms: []string{"Küche"}}, "HmIP-RF.WRC:1": {Name: "Taste oben"}}
	ts := Targets("HmIP-RF", devs(), objects)
	if len(ts) != 2 || ts[0].Address != "PDT:3" || ts[0].Family != profiles.HmIPDimmer || ts[0].Kind != profiles.Dimmer || ts[0].Device != "HmIP-PDT" || ts[0].Rooms[0] != "Küche" || ts[1].Kind != profiles.Blind {
		t.Fatalf("targets %+v", ts)
	}
	f := system.NewFake()
	f.Desc["HmIP-RF"] = litesource.ParamsetCache{"key": {"PRESS_SHORT": {}, "PRESS_LONG": {}}, "sw": {"STATE": {}}}
	f.DescKeys["HmIP-RF|WRC:1"] = "key"
	f.DescKeys["HmIP-RF|WRC:2"] = "key"
	f.DescKeys["HmIP-RF|SW:3"] = "sw"
	f.DescKeys["HmIP-RF|HmIP-RCV-1:1"] = "key"
	ss, err := Senders(context.Background(), f, "HmIP-RF", devs(), objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 || ss[0].Address != "WRC:1" || ss[0].Name != "Taste oben" || ss[1].Address != "WRC:2" {
		t.Fatalf("senders %+v", ss)
	}
	if ss, _ := Senders(context.Background(), f, "HmIP-RF", nil, objects); ss != nil {
		t.Error("senders of nothing")
	}
}

func TestForeign(t *testing.T) {
	f := system.NewFake()
	links := []system.Link{
		{Sender: "HmIP-RCV-1:10", Receiver: "PDT:3", Name: "DimUp"},
		{Sender: "HmIP-RCV-1:11", Receiver: "PDT:3", Name: "DimDown"},
		{Sender: "HmIP-RCV-1:12", Receiver: "SW:3"},  // not a target
		{Sender: "HmIP-RCV-1:13", Receiver: "PDT:3"}, // owned
		{Sender: "WRC:1", Receiver: "PDT:3"},         // not a virtual key
	}
	up, _ := profiles.Params(profiles.HmIPDimmer, profiles.UpOn, profiles.Options{Step: 0.03})
	f.SetParamset("HmIP-RF", "PDT:3", "HmIP-RCV-1:10", up)
	down, _ := profiles.Params(profiles.HmIPDimmer, profiles.DownOnly, profiles.Options{})
	f.SetParamset("HmIP-RF", "PDT:3", "HmIP-RCV-1:11", down)
	objects := system.Objects{"HmIP-RF.HmIP-RCV-1:10": {Name: "Licht DimUp"}}
	got := Foreign(context.Background(), f, "HmIP-RF", links, Targets("HmIP-RF", devs(), objects), objects, map[string]bool{"HmIP-RF|HmIP-RCV-1:13": true})
	if len(got) != 2 {
		t.Fatalf("foreign %+v", got)
	}
	if got[0].Key != "HmIP-RCV-1:10" || got[0].KeyName != "Licht DimUp" || got[0].Profile == nil || got[0].Profile.Role != profiles.UpOn || got[0].Profile.Step != 0.03 || got[0].Receiver.Address != "PDT:3" {
		t.Errorf("first %+v %+v", got[0], got[0].Profile)
	}
	if got[1].Key != "HmIP-RCV-1:11" || got[1].Profile.Role != profiles.DownOnly {
		t.Errorf("second %+v", got[1])
	}
	f.Fail["getParamset"] = context.DeadlineExceeded
	got = Foreign(context.Background(), f, "HmIP-RF", links[:1], Targets("HmIP-RF", devs(), objects), objects, nil)
	if len(got) != 1 || got[0].Error == "" || got[0].Profile != nil {
		t.Errorf("error not reported: %+v", got)
	}
}
