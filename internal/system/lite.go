package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// Lite is openccu-lite: lite-rpc for the processes, the metadata API for names and markers,
// both through ccu-addon-kit's litesource client (the addon's token).
type Lite struct {
	Client *litesource.Client
}

// NewLite makes the openccu-lite implementation over a litesource client.
func NewLite(c *litesource.Client) *Lite { return &Lite{Client: c} }

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

// SetObject implements System: the name and the addon's namespace through the kit's metadata
// write (nil removes the namespace; the store merges per namespace, and a write that changes
// nothing is not an error).
func (l *Lite) SetObject(ctx context.Context, iface, address string, name *string, marker *Marker) error {
	var ns any
	if marker != nil {
		ns = marker
	}
	_, _, err := l.Client.Meta().PatchObject(ctx, iface, address, litesource.ObjectPatch{Name: name, Meta: map[string]any{Namespace: ns}})
	if err != nil {
		return fmt.Errorf("metadata %s %s: %w", iface, address, err)
	}
	return nil
}
