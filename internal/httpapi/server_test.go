package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-longpressdim/docs"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/service"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/store"
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

func newServer(t *testing.T, level addonkit.Level) (http.Handler, *system.Fake, string) {
	t.Helper()
	f := system.NewFake()
	f.AddVirtualRemote("HmIP-RF")
	f.AddDevice("HmIP-RF", "PDT", "HmIP-PDT", "DIMMER_VIRTUAL_RECEIVER", "DIMMER_VIRTUAL_RECEIVER", "DIMMER_VIRTUAL_RECEIVER")
	f.AddDevice("BidCos-RF", "RC", "HM-RC-19", "KEY", "KEY")
	f.DefaultParamset = hmipDefault
	dir := t.TempDir()
	www := filepath.Join(dir, "app")
	os.MkdirAll(filepath.Join(www, "assets"), 0o755)
	os.WriteFile(filepath.Join(www, "index.html"), []byte("<html>app</html>"), 0o644)
	os.WriteFile(filepath.Join(www, "assets", "x.js"), []byte("js"), 0o644)
	st, _ := store.Open(dir)
	svc := service.New(service.Options{System: f, Store: st, Settings: store.OpenSettings(dir), Log: func(string, ...any) {}})
	wrap := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, addonkit.WithSession(r, &addonkit.SessionInfo{Level: level}))
		})
	}
	if level == addonkit.LevelUnknown {
		wrap = nil
	}
	h := New(Options{BasePath: "/addons/longpressdim", WWW: www, Service: svc, Wrap: wrap, OpenAPI: docs.OpenAPI, Version: "test", Platform: "openccu-lite", Started: time.Now()})
	return h, f, dir
}

func call(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, "http://box"+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, "http://box"+path, nil)
	}
	if method != http.MethodGet {
		r.Header.Set("X-CCU-Addon-Request", "1")
		r.Header.Set("Origin", "http://box")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("%s: %v", w.Body.String(), err)
	}
}

func TestRoutesMatchOpenAPI(t *testing.T) {
	newServer(t, addonkit.LevelAdmin)
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(docs.OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	param := regexp.MustCompile(`\{[a-z]+\}`)
	specRoutes := map[string]bool{}
	for p, ops := range spec.Paths {
		for m := range ops {
			if m == "parameters" {
				continue
			}
			specRoutes[strings.ToUpper(m)+" "+param.ReplaceAllString(p, "{}")] = true
		}
	}
	for _, r := range Routes {
		if !specRoutes[param.ReplaceAllString(r, "{}")] {
			t.Errorf("route %s is not in docs/openapi.json", r)
		}
		delete(specRoutes, param.ReplaceAllString(r, "{}"))
	}
	for r := range specRoutes {
		t.Errorf("docs/openapi.json has %s, the server does not", r)
	}
}

func TestFlow(t *testing.T) {
	h, f, _ := newServer(t, addonkit.LevelAdmin)
	w := call(h, "GET", "/addons/longpressdim/api/v1/health", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"test"`) {
		t.Fatalf("health %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"interfaces", "targets", "senders", "keys/HmIP-RF", "bindings", "foreign", "settings", "openapi.json"} {
		if w := call(h, "GET", "/addons/longpressdim/api/v1/"+p, nil); w.Code != 200 {
			t.Errorf("%s: %d %s", p, w.Code, w.Body)
		}
	}
	draft := map[string]any{"target": map[string]any{"interface": "HmIP-RF", "address": "PDT:3"}, "senders": []map[string]any{{"interface": "BidCos-RF", "address": "RC:1", "role": "up"}}}
	w = call(h, "POST", "/addons/longpressdim/api/v1/bindings/plan", draft)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"addLink"`) {
		t.Fatalf("plan %d %s", w.Code, w.Body)
	}
	if len(f.CallsOf("addLink")) != 0 {
		t.Error("the plan wrote")
	}
	w = call(h, "POST", "/addons/longpressdim/api/v1/bindings", draft)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	var res struct {
		Plan struct {
			Binding struct{ ID string }
		}
	}
	decodeBody(t, w, &res)
	id := res.Plan.Binding.ID
	if w := call(h, "GET", "/addons/longpressdim/api/v1/bindings/"+id, nil); w.Code != 200 {
		t.Errorf("get %d", w.Code)
	}
	if w := call(h, "GET", "/addons/longpressdim/api/v1/bindings/nope", nil); w.Code != 404 {
		t.Errorf("get unknown %d", w.Code)
	}
	w = call(h, "PATCH", "/addons/longpressdim/api/v1/bindings/"+id, map[string]any{"senders": []map[string]any{{"interface": "BidCos-RF", "address": "RC:2", "role": "both"}}, "step_interval_ms": 400})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"step_interval_ms":400`) {
		t.Errorf("patch %d %s", w.Code, w.Body)
	}
	if w := call(h, "PATCH", "/addons/longpressdim/api/v1/bindings/"+id, map[string]any{"senders": []map[string]any{{"interface": "HmIP-RF", "address": "PDT:1", "role": "up"}}}); w.Code != 422 {
		t.Errorf("bad patch %d", w.Code)
	}
	if w := call(h, "PATCH", "/addons/longpressdim/api/v1/bindings/nope", map[string]any{}); w.Code != 404 {
		t.Errorf("patch unknown %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings/"+id+"/press", map[string]any{"direction": "up", "datapoint": "PRESS_SHORT"}); w.Code != 200 {
		t.Errorf("press %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings/"+id+"/press", map[string]any{"direction": "left", "datapoint": "PRESS_SHORT"}); w.Code != 422 {
		t.Errorf("bad press %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings/nope/press", map[string]any{"direction": "up", "datapoint": "PRESS_SHORT"}); w.Code != 404 {
		t.Errorf("press unknown %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings/"+id+"/reapply", nil); w.Code != 200 {
		t.Errorf("reapply %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/check", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"active"`) {
		t.Errorf("check %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/longpressdim/api/v1/bindings/"+id+"/remove-plan?forget=1", nil); w.Code != 200 || strings.Contains(w.Body.String(), "removeLink") {
		t.Errorf("remove plan %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/import/parse", map[string]any{"text": "'a': 'b'"}); w.Code != 200 || !strings.Contains(w.Body.String(), "no channel named") {
		t.Errorf("parse %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/import/apply", map[string]any{"entries": []any{}}); w.Code != 200 {
		t.Errorf("import apply %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/foreign/adopt", map[string]any{"interface": "HmIP-RF", "target": "PDT:1", "up": "HmIP-RCV-1:1", "down": "HmIP-RCV-1:2"}); w.Code != 422 {
		t.Errorf("adopt nothing %d %s", w.Code, w.Body)
	}
	if w := call(h, "PUT", "/addons/longpressdim/api/v1/settings", map[string]any{"step_interval_ms": 450}); w.Code != 200 || !strings.Contains(w.Body.String(), "450") {
		t.Errorf("settings %d %s", w.Code, w.Body)
	}
	if w := call(h, "DELETE", "/addons/longpressdim/api/v1/bindings/"+id, nil); w.Code != 200 {
		t.Errorf("delete %d %s", w.Code, w.Body)
	}
	if w := call(h, "DELETE", "/addons/longpressdim/api/v1/bindings/"+id, nil); w.Code != 404 {
		t.Errorf("delete again %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings/remove-all", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"removed":0`) {
		t.Errorf("remove all %d %s", w.Code, w.Body)
	}
	// failures of the plan and of the system
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings", map[string]any{"target": map[string]any{"interface": "HmIP-RF", "address": "PDT:0"}}); w.Code != 422 {
		t.Errorf("bad create %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings", "nope"); w.Code != 400 {
		t.Errorf("bad json %d", w.Code)
	}
	f.Fail["addLink"] = context.DeadlineExceeded
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings", draft); w.Code != 502 {
		t.Errorf("failed create %d %s", w.Code, w.Body)
	}
	f.Fail["getLinks"] = context.DeadlineExceeded
	if w := call(h, "GET", "/addons/longpressdim/api/v1/keys/HmIP-RF", nil); w.Code != 504 {
		t.Errorf("timeout %d", w.Code)
	}
	if w := call(h, "GET", "/addons/longpressdim/api/v1/keys/VirtualDevices", nil); w.Code != 502 {
		t.Errorf("no remote %d", w.Code)
	}
}

func TestStaticAndGuards(t *testing.T) {
	h, _, _ := newServer(t, addonkit.LevelUser)
	if w := call(h, "GET", "/addons/longpressdim/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Errorf("index %d", w.Code)
	}
	if w := call(h, "GET", "/addons/longpressdim/bindings/x", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Errorf("spa fallback %d", w.Code)
	}
	w := call(h, "GET", "/addons/longpressdim/assets/x.js", nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset %d %s", w.Code, w.Header())
	}
	if w := call(h, "GET", "/addons/longpressdim/.hidden", nil); w.Code != 404 {
		t.Errorf("hidden %d", w.Code)
	}
	if w := call(h, "GET", "/addons/longpressdim/a/../../x", nil); w.Code != 404 {
		t.Errorf("unclean %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/index.html", nil); w.Code != 405 {
		t.Errorf("post static %d", w.Code)
	}
	if w := call(h, "GET", "/addons/longpressdim/api/v1/nope", nil); w.Code != 404 {
		t.Errorf("unknown api %d", w.Code)
	}
	// a write without the CSRF header
	r := httptest.NewRequest("POST", "http://box/addons/longpressdim/api/v1/check", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Errorf("csrf %d", rec.Code)
	}
	// a user may read and press, not write bindings or settings
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings", map[string]any{}); w.Code != 403 {
		t.Errorf("user create %d", w.Code)
	}
	if w := call(h, "PUT", "/addons/longpressdim/api/v1/settings", map[string]any{}); w.Code != 403 {
		t.Errorf("user settings %d", w.Code)
	}
	if w := call(h, "POST", "/addons/longpressdim/api/v1/bindings/remove-all", nil); w.Code != 403 {
		t.Errorf("user remove all %d", w.Code)
	}
	if w := call(h, "GET", "/addons/longpressdim/api/v1/settings", nil); w.Code != 200 {
		t.Errorf("user read %d", w.Code)
	}
	// without a session check (development) everything passes
	h2, _, _ := newServer(t, addonkit.LevelUnknown)
	if w := call(h2, "PUT", "/addons/longpressdim/api/v1/settings", map[string]any{}); w.Code != 200 {
		t.Errorf("dev settings %d", w.Code)
	}
}

func TestStream(t *testing.T) {
	h, f, _ := newServer(t, addonkit.LevelUser)
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/addons/longpressdim/api/v1/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %s", res.Header.Get("Content-Type"))
	}
	// the service is not running its stream here; broadcast through a check
	go func() {
		time.Sleep(50 * time.Millisecond)
		call(h, "POST", "/addons/longpressdim/api/v1/check", nil)
	}()
	buf := make([]byte, 4096)
	var got string
	for !strings.Contains(got, "event: check") {
		n, err := res.Body.Read(buf)
		if err != nil {
			t.Fatalf("read: %v (%q)", err, got)
		}
		got += string(buf[:n])
	}
	_ = f
}
