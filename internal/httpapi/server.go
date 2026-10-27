// Package httpapi is the addon's own HTTP server: the Svelte app's files and the JSON API under
// one base path (/addons/longpressdim/), which lighttpd proxies to the loopback port. Every
// request carries the gate's session header, confirmed through addonkit; writes carry the kit's
// CSRF header. docs/api.md documents the routes and docs/openapi.json (served at
// api/v1/openapi.json) specifies them.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/service"
)

// Options configure the server.
type Options struct {
	// BasePath is where the app is served, /addons/longpressdim (no trailing slash).
	BasePath string
	// WWW is the directory with the built app (index.html and its assets).
	WWW     string
	Service *service.Service
	// Wrap is the session check as a handler wrapper (openccu-lite's or a classic CCU's); nil:
	// no check, development only.
	Wrap func(http.Handler) http.Handler
	// OpenAPI is the spec served at api/v1/openapi.json.
	OpenAPI  []byte
	Version  string
	Platform string
	Started  time.Time
}

// Routes lists every "METHOD /path" the API serves; a test holds docs/openapi.json to it.
var Routes []string

func handle(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	for _, r := range Routes {
		if r == pattern {
			mux.HandleFunc(pattern, h)
			return
		}
	}
	Routes = append(Routes, pattern)
	mux.HandleFunc(pattern, h)
}

// New builds the handler.
func New(o Options) http.Handler {
	o.BasePath = strings.TrimRight(o.BasePath, "/")
	api := http.NewServeMux()
	handle(api, "GET /api/v1/health", o.health)
	handle(api, "GET /api/v1/openapi.json", o.openapi)
	handle(api, "GET /api/v1/interfaces", o.interfaces)
	handle(api, "GET /api/v1/targets", o.targets)
	handle(api, "GET /api/v1/senders", o.senders)
	handle(api, "GET /api/v1/keys/{interface}", o.keys)
	handle(api, "GET /api/v1/bindings", o.bindings)
	handle(api, "POST /api/v1/bindings/plan", o.plan)
	handle(api, "POST /api/v1/bindings", o.create)
	handle(api, "GET /api/v1/bindings/{id}", o.binding)
	handle(api, "PATCH /api/v1/bindings/{id}", o.patch)
	handle(api, "DELETE /api/v1/bindings/{id}", o.remove)
	handle(api, "GET /api/v1/bindings/{id}/remove-plan", o.removePlan)
	handle(api, "POST /api/v1/bindings/{id}/reapply", o.reapply)
	handle(api, "POST /api/v1/bindings/{id}/press", o.press)
	handle(api, "POST /api/v1/bindings/remove-all", o.removeAll)
	handle(api, "GET /api/v1/foreign", o.foreign)
	handle(api, "POST /api/v1/foreign/adopt", o.adopt)
	handle(api, "POST /api/v1/import/parse", o.importParse)
	handle(api, "POST /api/v1/import/apply", o.importApply)
	handle(api, "POST /api/v1/check", o.check)
	handle(api, "GET /api/v1/settings", o.settings)
	handle(api, "PUT /api/v1/settings", o.putSettings)
	handle(api, "GET /api/v1/stream", o.stream)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		addonkit.WriteError(w, http.StatusNotFound, "not-found", r.URL.Path)
	})

	root := http.NewServeMux()
	root.Handle("/api/", addonkit.CSRF(api))
	root.Handle("/", http.HandlerFunc(o.static))

	var h http.Handler = http.StripPrefix(o.BasePath, root)
	if o.Wrap != nil {
		h = o.Wrap(h)
	}
	return guard(h)
}

// guard refuses a path that is not clean (the mux would redirect it, and after StripPrefix the
// redirect would point outside the base path) and marks API answers as not cacheable.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p != "/" && (path.Clean(p) != strings.TrimSuffix(p, "/") && path.Clean(p) != p) {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(v); err != nil {
		addonkit.WriteError(w, http.StatusBadRequest, "invalid", err.Error())
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		addonkit.WriteError(w, http.StatusNotFound, "not-found", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		addonkit.WriteError(w, http.StatusGatewayTimeout, "timeout", err.Error())
	default:
		addonkit.WriteError(w, http.StatusBadGateway, "system", err.Error())
	}
}

// admin refuses a request whose session is below the administrator level where the session is
// known (openccu-lite); without a session check (development) everything passes.
func admin(w http.ResponseWriter, r *http.Request) bool {
	s := addonkit.FromRequest(r)
	if s == nil {
		return true
	}
	if !addonkit.Admits(addonkit.LevelAdmin, s.Level) {
		addonkit.WriteError(w, http.StatusForbidden, "forbidden", "administrators only")
		return false
	}
	return true
}

func (o Options) health(w http.ResponseWriter, r *http.Request) {
	h := o.Service.Health()
	writeJSON(w, 200, map[string]any{
		"ok":       h.Stream,
		"version":  o.Version,
		"platform": o.Platform,
		"uptime_s": int(time.Since(o.Started).Seconds()),
		"service":  h,
	})
}

func (o Options) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(o.OpenAPI)
}

func (o Options) interfaces(w http.ResponseWriter, r *http.Request) {
	l, err := o.Service.Interfaces(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"interfaces": l})
}

func (o Options) targets(w http.ResponseWriter, r *http.Request) {
	l, err := o.Service.Targets(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"targets": l})
}

func (o Options) senders(w http.ResponseWriter, r *http.Request) {
	l, err := o.Service.Senders(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"senders": l})
}

func (o Options) keys(w http.ResponseWriter, r *http.Request) {
	l, err := o.Service.Keys(r.Context(), r.PathValue("interface"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"keys": l})
}

func (o Options) bindings(w http.ResponseWriter, r *http.Request) {
	l, err := o.Service.Bindings(r.Context())
	if err != nil && l == nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"bindings": l})
}

func (o Options) binding(w http.ResponseWriter, r *http.Request) {
	v, ok := o.Service.Binding(r.Context(), r.PathValue("id"))
	if !ok {
		addonkit.WriteError(w, http.StatusNotFound, "not-found", r.PathValue("id"))
		return
	}
	writeJSON(w, 200, v)
}

func (o Options) plan(w http.ResponseWriter, r *http.Request) {
	var b model.Binding
	if !decode(w, r, &b) {
		return
	}
	p, err := o.Service.Plan(r.Context(), b)
	if err != nil {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "plan", err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (o Options) create(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var b model.Binding
	if !decode(w, r, &b) {
		return
	}
	res, err := o.Service.Apply(r.Context(), b)
	if err != nil {
		if res == nil {
			addonkit.WriteError(w, http.StatusUnprocessableEntity, "plan", err.Error())
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "apply", "message": err.Error(), "result": res})
		return
	}
	writeJSON(w, 201, res)
}

type patchBody struct {
	Senders        []model.Sender `json:"senders"`
	StepIntervalMS int            `json:"step_interval_ms"`
}

func (o Options) patch(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var b patchBody
	if !decode(w, r, &b) {
		return
	}
	v, err := o.Service.SetSenders(r.Context(), r.PathValue("id"), b.Senders, b.StepIntervalMS)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			fail(w, err)
			return
		}
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
		return
	}
	writeJSON(w, 200, v)
}

func (o Options) remove(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	forget := r.URL.Query().Get("forget") == "1" || r.URL.Query().Get("forget") == "true"
	res, err := o.Service.Remove(r.Context(), r.PathValue("id"), forget)
	if err != nil {
		if res == nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "apply", "message": err.Error(), "result": res})
		return
	}
	writeJSON(w, 200, res)
}

func (o Options) removePlan(w http.ResponseWriter, r *http.Request) {
	forget := r.URL.Query().Get("forget") == "1" || r.URL.Query().Get("forget") == "true"
	p, err := o.Service.PlanRemove(r.Context(), r.PathValue("id"), forget)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (o Options) reapply(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	res, err := o.Service.Reapply(r.Context(), r.PathValue("id"))
	if err != nil {
		if res == nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "apply", "message": err.Error(), "result": res})
		return
	}
	writeJSON(w, 200, res)
}

func (o Options) press(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Direction string `json:"direction"`
		Datapoint string `json:"datapoint"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Direction != "up" && b.Direction != "down" {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid", "direction up or down")
		return
	}
	if err := o.Service.TestPress(r.Context(), r.PathValue("id"), b.Direction, b.Datapoint); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			fail(w, err)
			return
		}
		addonkit.WriteError(w, http.StatusBadGateway, "press", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (o Options) removeAll(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	forget := r.URL.Query().Get("forget") == "1" || r.URL.Query().Get("forget") == "true"
	n, err := o.Service.RemoveAll(r.Context(), forget)
	out := map[string]any{"removed": n}
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, http.StatusBadGateway, out)
		return
	}
	writeJSON(w, 200, out)
}

func (o Options) foreign(w http.ResponseWriter, r *http.Request) {
	l, err := o.Service.Foreign(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"links": l})
}

type adoptBody struct {
	Interface string         `json:"interface"`
	Target    string         `json:"target"`
	Up        string         `json:"up"`
	Down      string         `json:"down"`
	Senders   []model.Sender `json:"senders"`
}

func (o Options) adopt(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var b adoptBody
	if !decode(w, r, &b) {
		return
	}
	res, err := o.Service.Adopt(r.Context(), b.Interface, b.Target, b.Up, b.Down, b.Senders)
	if err != nil {
		if res == nil {
			addonkit.WriteError(w, http.StatusUnprocessableEntity, "adopt", err.Error())
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "apply", "message": err.Error(), "result": res})
		return
	}
	writeJSON(w, 201, res)
}

func (o Options) importParse(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &b) {
		return
	}
	entries, err := o.Service.ParseMapping(r.Context(), b.Text)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"entries": entries})
}

func (o Options) importApply(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var b struct {
		Entries []service.MappingEntry `json:"entries"`
	}
	if !decode(w, r, &b) {
		return
	}
	added, skipped, err := o.Service.ApplyMapping(r.Context(), b.Entries)
	out := map[string]any{"added": added, "skipped": skipped}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, 200, out)
}

func (o Options) check(w http.ResponseWriter, r *http.Request) {
	o.Service.Check(r.Context())
	l, _ := o.Service.Bindings(r.Context())
	writeJSON(w, 200, map[string]any{"bindings": l})
}

func (o Options) settings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, o.Service.Settings())
}

func (o Options) putSettings(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var v model.Settings
	if !decode(w, r, &v) {
		return
	}
	st, err := o.Service.SetSettings(v)
	if err != nil {
		addonkit.WriteError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

// stream is a server-sent event stream of the service's events (presses of any key channel,
// level reports, binding changes) for the editor's "press the button now" and the live list.
func (o Options) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		addonkit.WriteError(w, http.StatusNotImplemented, "stream", "no flusher")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	fmt.Fprint(w, ": hello\n\n")
	fl.Flush()
	ch, cancel := o.Service.Subscribe()
	defer cancel()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
			fl.Flush()
		}
	}
}

func (o Options) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		addonkit.WriteError(w, http.StatusMethodNotAllowed, "method", r.Method)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	file := filepath.Join(o.WWW, filepath.FromSlash(p))
	if st, err := os.Stat(file); err == nil && !st.IsDir() {
		if strings.HasPrefix(p, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, file)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(o.WWW, "index.html"))
}
