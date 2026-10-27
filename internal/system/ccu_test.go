package system

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hobbyquaker/ccu-addon-kit/go/ccusource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// a classic CCU in miniature: one XML-RPC process (listMethods, listDevices, getLinks,
// putParamset, setValue, reportValueUsage) and a rega.exe that answers the names script and
// the rename script
type miniCCU struct {
	mu      sync.Mutex
	calls   []string
	params  map[string]any
	renamed map[string]string
	regaOut string
}

func (m *miniCCU) xmlrpc(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	method, params, err := ccusource.UnmarshalCall(b)
	if err != nil {
		w.Write(ccusource.MarshalFault(-1, err.Error()))
		return
	}
	m.mu.Lock()
	m.calls = append(m.calls, method)
	m.mu.Unlock()
	var out any = ""
	switch method {
	case "system.listMethods":
		out = []any{"listDevices"}
	case "listDevices":
		out = []any{
			map[string]any{"ADDRESS": "DIM", "TYPE": "HMW-LC-Dim1L-DR", "CHILDREN": []any{"DIM:3"}},
			map[string]any{"ADDRESS": "DIM:3", "TYPE": "HMW_DIMMER", "PARENT": "DIM"},
		}
	case "getLinks":
		out = []any{map[string]any{"SENDER": "BidCoS-Wir:50", "RECEIVER": "DIM:3", "NAME": "x", "DESCRIPTION": "", "FLAGS": 0}}
	case "putParamset":
		m.mu.Lock()
		m.params, _ = params[2].(map[string]any)
		m.mu.Unlock()
	case "reportValueUsage":
		out = true
	case "boom":
		w.Write(ccusource.MarshalFault(-2, "nope"))
		return
	}
	res, _ := ccusource.MarshalResponse(out)
	w.Write(res)
}

func (m *miniCCU) rega(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	script := string(b)
	if strings.Contains(script, "oCh.Name(sName)") {
		// the rename script: take the quoted name and address
		addr := between(script, `string sAddr = "`, `"`)
		name := between(script, `string sName = "`, `"`)
		m.mu.Lock()
		if m.renamed == nil {
			m.renamed = map[string]string{}
		}
		m.renamed[addr] = name
		m.mu.Unlock()
		if addr == "BidCoS-Wir:49" {
			io.WriteString(w, "missing\n<xml>")
			return
		}
		io.WriteString(w, "ok\n<xml>")
		return
	}
	io.WriteString(w, m.regaOut+"<xml>")
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	if j < 0 {
		return s
	}
	return s[:j]
}

func newMini(t *testing.T) (*CCU, *miniCCU, string) {
	t.Helper()
	m := &miniCCU{regaOut: "D\tBidCos-Wired\tDIM\tDimmer Flur\nC\tDIM:3\tLicht Flur\t\nC\tBidCoS-Wir:50\tBidCoS-Wir:50\t\nD\tBidCos-Wired\tBidCoS-Wir\tHMW-RCV-50\n"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "rega.exe") {
			m.rega(w, r)
			return
		}
		m.xmlrpc(w, r)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	list := filepath.Join(dir, "InterfacesList.xml")
	os.WriteFile(list, []byte(`<interfaces><ipc><name>BidCos-Wired</name><url>`+strings.Replace(srv.URL, "http://", "xmlrpc://", 1)+`</url></ipc></interfaces>`), 0o644)
	src := ccusource.New(ccusource.Options{ID: "lpd", InterfacesList: list, Rega: srv.URL + "/rega.exe", Log: func(string, ...any) {}})
	return NewCCU(src, dir), m, dir
}

func TestCCUCallAndObjects(t *testing.T) {
	c, m, dir := newMini(t)
	ctx := context.Background()
	ifs, err := c.Interfaces(ctx)
	if err != nil || len(ifs) != 1 || ifs[0].Name != "BidCos-Wired" {
		t.Fatalf("interfaces %v %+v", err, ifs)
	}
	devs, err := c.Devices(ctx, "BidCos-Wired")
	if err != nil || len(devs) != 2 {
		t.Fatalf("devices %v %d", err, len(devs))
	}
	links, err := Links(ctx, c, "BidCos-Wired")
	if err != nil || len(links) != 1 || links[0].Sender != "BidCoS-Wir:50" {
		t.Fatalf("links %v %+v", err, links)
	}
	if err := PutLinkParamset(ctx, c, "BidCos-Wired", "DIM:3", "BidCoS-Wir:50", map[string]any{"LONG_DIM_STEP": 0.03, "LONG_ACTION_TYPE": 4, "LONG_MULTIEXECUTE": true}); err != nil {
		t.Fatal(err)
	}
	if m.params["LONG_DIM_STEP"] != 0.03 || m.params["LONG_ACTION_TYPE"] != 4 || m.params["LONG_MULTIEXECUTE"] != true {
		t.Errorf("params %v", m.params)
	}
	raw, err := c.Call(ctx, "BidCos-Wired", "reportValueUsage", "K:1", "PRESS_SHORT", 1)
	if err != nil || string(raw) != "true" {
		t.Errorf("usage %s %v", raw, err)
	}
	if _, err := c.Call(ctx, "BidCos-Wired", "boom"); err == nil {
		t.Error("fault not reported")
	}
	if _, err := c.Call(ctx, "Nope", "getLinks"); err == nil {
		t.Error("unknown interface")
	}
	// names from ReGa, no markers yet
	objs, err := c.Objects(ctx, "de")
	if err != nil {
		t.Fatal(err)
	}
	if objs.Get("BidCos-Wired", "DIM:3").Name != "Licht Flur" || objs.Get("BidCos-Wired", "BidCoS-Wir:50").Marker() != nil {
		t.Errorf("objects %+v", objs)
	}
	// a marker and a name
	n := "Licht Flur ▲"
	if err := c.SetObject(ctx, "BidCos-Wired", "BidCoS-Wir:50", &n, &Marker{Binding: "dim-3", Role: "up"}); err != nil {
		t.Fatal(err)
	}
	if m.renamed["BidCoS-Wir:50"] != n {
		t.Errorf("renamed %v", m.renamed)
	}
	objs, _ = c.Objects(ctx, "de")
	if mk := objs.Get("BidCos-Wired", "BidCoS-Wir:50").Marker(); mk == nil || mk.Binding != "dim-3" {
		t.Errorf("marker %+v", mk)
	}
	// the file survives a new instance
	c2 := NewCCU(c.Src, dir)
	objs, _ = c2.Objects(ctx, "de")
	if objs.Get("BidCos-Wired", "BidCoS-Wir:50").Marker() == nil {
		t.Error("marker not persisted")
	}
	// a marker on a key ReGa does not list is still an object
	if err := c2.SetObject(ctx, "BidCos-Wired", "BidCoS-Wir:48", nil, &Marker{Binding: "x", Role: "down"}); err != nil {
		t.Fatal(err)
	}
	objs, _ = c2.Objects(ctx, "de")
	if objs.Get("BidCos-Wired", "BidCoS-Wir:48").Marker() == nil {
		t.Error("marker without a ReGa object")
	}
	// removed
	if err := c2.SetObject(ctx, "BidCos-Wired", "BidCoS-Wir:50", nil, nil); err != nil {
		t.Fatal(err)
	}
	objs, _ = c2.Objects(ctx, "de")
	if objs.Get("BidCos-Wired", "BidCoS-Wir:50").Marker() != nil {
		t.Error("marker not removed")
	}
	// a rename of a channel ReGa does not know
	if err := c2.SetObject(ctx, "BidCos-Wired", "BidCoS-Wir:49", &n, nil); err == nil {
		t.Error("missing channel renamed")
	}
	// a damaged markers file is an error, not an empty set
	os.WriteFile(filepath.Join(dir, "var", "markers.json"), []byte("{"), 0o644)
	c3 := NewCCU(c.Src, dir)
	if _, err := c3.Objects(ctx, "de"); err == nil {
		t.Error("damaged markers accepted")
	}
}

func TestXMLRPCValue(t *testing.T) {
	v := xmlrpcValue(map[string]any{"a": litesource.Double(0.5), "b": []any{litesource.Double(1), 2}, "c": "x"})
	m := v.(map[string]any)
	if m["a"] != 0.5 || m["b"].([]any)[0] != 1.0 || m["b"].([]any)[1] != 2 || m["c"] != "x" {
		t.Errorf("%v", m)
	}
	if regaString(`a"b\c`) != `"a\"b\\c"` {
		t.Error(regaString(`a"b\c`))
	}
	b, _ := json.Marshal(Marker{Binding: "x", Role: "up"})
	if string(b) != `{"binding":"x","role":"up"}` {
		t.Error(string(b))
	}
}
