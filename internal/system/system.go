// Package system is the seam between the addon and the system it runs on: the interface
// processes' RPC (links, paramsets, presses), the event stream, and the names and markers of the
// channels. openccu-lite's implementation is Lite (lite-rpc and the metadata API through
// ccu-addon-kit's litesource); a classic CCU's comes with the classic phase. Fake is the test
// double every package tests against.
package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// Namespace is the addon's namespace in the channel metadata (meta.longpressdim).
const Namespace = "longpressdim"

// Marker is what the addon writes into a key's metadata: the key belongs to a binding.
type Marker struct {
	Binding string `json:"binding"`
	Role    string `json:"role"`
}

// Object is a channel's metadata as the addon reads it: its name and the namespaces present.
type Object struct {
	Name string
	// Meta holds every namespace of the object; the addon's own is under Namespace.
	Meta map[string]json.RawMessage
	// Rooms are the names of the object's rooms in the UI's language, where the system has them.
	Rooms []string
}

// Marker decodes the addon's namespace, nil when absent.
func (o Object) Marker() *Marker {
	raw, ok := o.Meta[Namespace]
	if !ok || string(raw) == "null" {
		return nil
	}
	var m Marker
	if json.Unmarshal(raw, &m) != nil || m.Binding == "" {
		return nil
	}
	return &m
}

// Foreign tells whether another consumer marked the object (a namespace that is not ours).
func (o Object) Foreign() bool {
	for ns := range o.Meta {
		if ns != Namespace {
			return true
		}
	}
	return false
}

// Objects is the metadata of every channel, keyed <interface>.<address>.
type Objects map[string]Object

// Get answers an object, the zero Object when unknown.
func (o Objects) Get(iface, address string) Object { return o[iface+"."+address] }

// Link is one entry of an interface process's getLinks().
type Link struct {
	Sender      string `json:"SENDER"`
	Receiver    string `json:"RECEIVER"`
	Name        string `json:"NAME"`
	Description string `json:"DESCRIPTION"`
	Flags       int    `json:"FLAGS"`
}

// System is what the addon needs of the system.
type System interface {
	// Interfaces names the interface processes.
	Interfaces(ctx context.Context) ([]litesource.Interface, error)
	// Devices lists the devices and channels of an interface.
	Devices(ctx context.Context, iface string) ([]litesource.DeviceDescription, error)
	// Call sends one RPC call to an interface process. A float64 parameter is sent as a double.
	Call(ctx context.Context, iface, method string, params ...any) (json.RawMessage, error)
	// Subscribe delivers the live events of the filter's addresses; a Resync message means the
	// stream lost its place (the relay needs nothing then: presses are not replayed).
	Subscribe(ctx context.Context, f litesource.Filter, since string) <-chan litesource.Message
	// Objects reads the names and markers of every channel.
	Objects(ctx context.Context, lang string) (Objects, error)
	// SetObject writes a channel's name (nil: unchanged) and the addon's marker (nil: removed).
	SetObject(ctx context.Context, iface, address string, name *string, marker *Marker) error
	// Descriptions answers the VALUES descriptions of addresses, each once by its key.
	Descriptions(ctx context.Context, iface string, addresses []string) (keys map[string]string, sets litesource.ParamsetCache, err error)
}

// --- helpers over Call --------------------------------------------------------------------

// Links reads every link of an interface: getLinks() without arguments, the one form every
// process answers (hs485d answers [] for a channel's own address).
func Links(ctx context.Context, s System, iface string) ([]Link, error) {
	raw, err := s.Call(ctx, iface, "getLinks")
	if err != nil {
		return nil, err
	}
	var out []Link
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("getLinks on %s: %w", iface, err)
	}
	return out, nil
}

// LinkParamset reads the receiver-side LINK paramset of a key: getParamset(<receiver>, <key>)
// (the reverse order answers an empty set).
func LinkParamset(ctx context.Context, s System, iface, receiver, key string) (map[string]json.RawMessage, error) {
	raw, err := s.Call(ctx, iface, "getParamset", receiver, key)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("getParamset(%s, %s): %w", receiver, key, err)
	}
	return out, nil
}

// PutLinkParamset writes link parameters into the receiver: putParamset(<receiver>, <key>, {…}).
// Floats go as doubles, integers as integers, flags as booleans.
func PutLinkParamset(ctx context.Context, s System, iface, receiver, key string, params map[string]any) error {
	_, err := s.Call(ctx, iface, "putParamset", receiver, key, Typed(params))
	return err
}

// Typed converts a parameter map into what Call sends: float64 as Double (an XML-RPC double
// whatever it looks like), the rest as it is.
func Typed(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		if f, ok := v.(float64); ok {
			out[k] = litesource.Double(f)
		} else {
			out[k] = v
		}
	}
	return out
}

// AddLink creates a link from a key to a receiver.
func AddLink(ctx context.Context, s System, iface, key, receiver, name, description string) error {
	_, err := s.Call(ctx, iface, "addLink", key, receiver, name, description)
	return err
}

// RemoveLink removes a link from a key to a receiver.
func RemoveLink(ctx context.Context, s System, iface, key, receiver string) error {
	_, err := s.Call(ctx, iface, "removeLink", key, receiver)
	return err
}

// Press presses a virtual key: setValue(<key>, PRESS_SHORT|PRESS_LONG, true).
func Press(ctx context.Context, s System, iface, key, datapoint string) error {
	_, err := s.Call(ctx, iface, "setValue", key, datapoint, true)
	return err
}

// ConfigPending reads CONFIG_PENDING of a device's channel 0 (from the process's cache on HmIP,
// a free read on BidCos as well); false when the device has no such datapoint.
func ConfigPending(ctx context.Context, s System, iface, device string) (bool, error) {
	raw, err := s.Call(ctx, iface, "getParamset", device+":0", "VALUES")
	if err != nil {
		return false, err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return false, err
	}
	v, ok := values["CONFIG_PENDING"]
	if !ok {
		return false, nil
	}
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return b, nil
	}
	var n float64
	if json.Unmarshal(v, &n) == nil {
		return n != 0, nil
	}
	return false, nil
}

// IsTimeout tells an interface process's "Generic error (TIMEOUT)" apart from other faults: on
// HmIP an addLink or putParamset that timed out is pending on the device, not failed.
func IsTimeout(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "TIMEOUT")
}

// DeviceOf answers the device address of a channel address.
func DeviceOf(address string) string {
	if i := strings.IndexByte(address, ':'); i >= 0 {
		return address[:i]
	}
	return address
}
