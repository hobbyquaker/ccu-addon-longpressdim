package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
)

func binding(id string) model.Binding {
	return model.Binding{ID: id, Target: model.Target{Interface: "HmIP-RF", Address: "ABC:3", Type: "DIMMER_VIRTUAL_RECEIVER", Family: "hmip-dimmer"}, Mode: model.TwoKey, Up: "HmIP-RCV-1:50", Down: "HmIP-RCV-1:49"}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Fatal("not empty")
	}
	if err := s.Put(binding("b")); err != nil {
		t.Fatal(err)
	}
	a := binding("a")
	a.Up, a.Down = "HmIP-RCV-1:48", "HmIP-RCV-1:47"
	if err := s.Put(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "var", "bindings.json")); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	l := s2.List()
	if len(l) != 2 || l[0].ID != "a" || l[1].ID != "b" || l[0].Created.IsZero() || l[0].Updated.IsZero() {
		t.Fatalf("list %+v", l)
	}
	if id, role := s2.KeyOwner("HmIP-RF", "HmIP-RCV-1:47"); id != "a" || role != "down" {
		t.Errorf("owner %s %s", id, role)
	}
	if id, _ := s2.KeyOwner("BidCos-RF", "HmIP-RCV-1:49"); id != "" {
		t.Error("owner on another interface")
	}
	if err := s2.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if err := s2.Delete("nope"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get("a"); ok {
		t.Error("still there")
	}
	s3, _ := Open(dir)
	if len(s3.List()) != 1 {
		t.Error("delete not written")
	}
	if s3.Path() == "" {
		t.Error("path")
	}
}

func TestStoreClonesSenders(t *testing.T) {
	s, _ := Open(t.TempDir())
	b := binding("a")
	b.Senders = []model.Sender{{Interface: "HmIP-RF", Address: "K:1", Role: model.RoleUp}}
	s.Put(b)
	got, _ := s.Get("a")
	got.Senders[0].Usage = &model.Usage{State: model.UsageReported}
	if b.Senders[0].Usage != nil {
		t.Error("the caller's draft shares the store's slice")
	}
	if again, _ := s.Get("a"); again.Senders[0].Usage != nil {
		t.Error("a change on a copy reached the store")
	}
	s.Put(got)
	l := s.List()
	l[0].Senders[0].Usage.State = "x"
	if again, _ := s.Get("a"); again.Senders[0].Usage.State != model.UsageReported {
		t.Error("a change on a listed copy reached the store")
	}
}

func TestStoreRefusesDamageAndNewerFormat(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "var"), 0o755)
	os.WriteFile(filepath.Join(dir, "var", "bindings.json"), []byte("{nope"), 0o644)
	if _, err := Open(dir); err == nil {
		t.Error("damaged file accepted")
	}
	os.WriteFile(filepath.Join(dir, "var", "bindings.json"), []byte(`{"format": 99, "bindings": []}`), 0o644)
	if _, err := Open(dir); err == nil {
		t.Error("newer format accepted")
	}
}

func TestStoreWriteFailureKeepsMemory(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	// make var/ a file so the write fails
	os.WriteFile(filepath.Join(dir, "var"), []byte("x"), 0o644)
	if err := s.Put(binding("a")); err == nil {
		t.Fatal("write succeeded into a file")
	}
	if len(s.List()) != 0 {
		t.Error("memory changed although the write failed")
	}
}

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	s := OpenSettings(dir)
	if s.Get() != model.DefaultSettings() {
		t.Fatalf("defaults %+v", s.Get())
	}
	if err := s.Set(model.Settings{StepIntervalMS: 500, HoldTimeoutMS: 10, LogPresses: true}); err != nil {
		t.Fatal(err)
	}
	got := OpenSettings(dir).Get()
	if got.StepIntervalMS != 500 || got.HoldTimeoutMS != 1000 || !got.LogPresses {
		t.Errorf("%+v", got)
	}
	os.WriteFile(filepath.Join(dir, "etc", "longpressdim.json"), []byte("garbage"), 0o644)
	if OpenSettings(dir).Get() != model.DefaultSettings() {
		t.Error("damaged settings not defaulted")
	}
}
