package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// Lite is openccu-lite: lite-rpc for the processes, the metadata API for names and markers.
type Lite struct {
	Client *litesource.Client
	// HTTP is the client for the metadata writes (the addon's token); nil: one from Token.
	HTTP  *http.Client
	Token addonkit.TokenSource
}

// NewLite makes the openccu-lite implementation over a litesource client and its token.
func NewLite(c *litesource.Client, token addonkit.TokenSource) *Lite {
	return &Lite{Client: c, Token: token, HTTP: &http.Client{Transport: &addonkit.AuthTransport{Source: token}, Timeout: 15 * time.Second}}
}

var _ System = (*Lite)(nil)

// Interfaces implements System.
func (l *Lite) Interfaces(ctx context.Context) ([]litesource.Interface, error) {
	return l.Client.Interfaces(ctx)
}

// Devices implements System.
func (l *Lite) Devices(ctx context.Context, iface string) ([]litesource.DeviceDescription, error) {
	return l.Client.ListDevices(ctx, iface)
}

// Call implements System.
func (l *Lite) Call(ctx context.Context, iface, method string, params ...any) (json.RawMessage, error) {
	return l.Client.Call(ctx, iface, method, params...)
}

// Subscribe implements System.
func (l *Lite) Subscribe(ctx context.Context, f litesource.Filter, since string) <-chan litesource.Message {
	return l.Client.Events(ctx, f, since)
}

// Descriptions implements System.
func (l *Lite) Descriptions(ctx context.Context, iface string, addresses []string) (map[string]string, litesource.ParamsetCache, error) {
	return l.Client.Descriptions(ctx, iface, addresses)
}

// Objects implements System over the metadata snapshot.
func (l *Lite) Objects(ctx context.Context, lang string) (Objects, error) {
	names, err := l.Client.Names(ctx)
	if err != nil {
		return nil, err
	}
	out := make(Objects, len(names.Objects))
	for ref, o := range names.Objects {
		i := strings.IndexByte(ref, '.')
		if i < 0 {
			continue
		}
		out[ref] = Object{Name: o.Name, Meta: o.Meta, Rooms: names.RoomNames(ref[:i], ref[i+1:], lang)}
	}
	return out, nil
}

// SetObject implements System: PATCH /api/meta/v1/objects/{ref} with the name and the addon's
// namespace (null removes it; the store merges per namespace).
func (l *Lite) SetObject(ctx context.Context, iface, address string, name *string, marker *Marker) error {
	body := map[string]any{"meta": map[string]any{Namespace: marker}}
	if name != nil {
		body["name"] = *name
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := strings.TrimRight(l.Client.Base(), "/") + "/api/meta/v1/objects/" + url.PathEscape(iface+"."+address)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
	res, err := l.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("metadata %s %s: %s: %s", iface, address, res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
