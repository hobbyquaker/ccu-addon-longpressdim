package system

import (
	"context"
	"strings"
	"testing"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource/fakelite"
)

// Lite against the kit's fake system: the marker and the name go through the kit's metadata
// write, read back through the snapshot; a write that changes nothing is fine.
func TestLiteObjects(t *testing.T) {
	s := fakelite.New()
	defer s.Close()
	s.Token = "olt_x"
	l := NewLite(litesource.New(s.URL, addonkit.StaticToken("olt_x")))
	ctx := context.Background()
	s.SetName("BidCos-RF.BidCoS-RF:48", "BidCoS-RF:48")

	name := "Flur oben"
	m := &Marker{Binding: "b1", Role: "up"}
	if err := l.SetObject(ctx, "BidCos-RF", "BidCoS-RF:48", &name, m); err != nil {
		t.Fatal(err)
	}
	if err := l.SetObject(ctx, "BidCos-RF", "BidCoS-RF:48", nil, m); err != nil {
		t.Fatalf("an unchanged write is not an error: %v", err)
	}
	objs, err := l.Objects(ctx, "de")
	if err != nil {
		t.Fatal(err)
	}
	o := objs.Get("BidCos-RF", "BidCoS-RF:48")
	if o.Name != name || o.Marker() == nil || *o.Marker() != *m || o.Foreign() {
		t.Fatalf("object: %+v", o)
	}
	// nil removes the namespace; the body says null for it and carries no name
	if err := l.SetObject(ctx, "BidCos-RF", "BidCoS-RF:48", nil, nil); err != nil {
		t.Fatal(err)
	}
	if w := s.MetaWrites[len(s.MetaWrites)-1]; !strings.Contains(w, `"longpressdim":null`) || strings.Contains(w, `"name"`) {
		t.Fatalf("removal body: %s", w)
	}
	objs, _ = l.Objects(ctx, "de")
	if objs.Get("BidCos-RF", "BidCoS-RF:48").Marker() != nil {
		t.Fatal("marker not removed")
	}
	// a refusal names the channel
	empty := ""
	if err := l.SetObject(ctx, "BidCos-RF", "BidCoS-RF:48", &empty, nil); err == nil || !strings.Contains(err.Error(), "BidCoS-RF:48") {
		t.Fatalf("empty name: %v", err)
	}
}
