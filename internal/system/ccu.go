package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hobbyquaker/ccu-addon-kit/go/ccusource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// CCU is a classic CCU (OpenCCU, the CCU3): the interface processes over XML-RPC on the
// loopback (ccusource's callback server for the events), ReGa for the names and rooms, and
// the addon's own file for the markers - a CCU has no metadata store, so the ownership of a
// virtual key lives in var/markers.json beside the bindings, and the key's name in ReGa.
type CCU struct {
	Src *ccusource.Source
	// MarkersFile is var/markers.json.
	MarkersFile string
	mu          sync.Mutex
	markers     map[string]Marker // ref -> marker
	loaded      bool
}

// NewCCU makes the classic implementation over a ccusource.
func NewCCU(src *ccusource.Source, addonDir string) *CCU {
	return &CCU{Src: src, MarkersFile: filepath.Join(addonDir, "var", "markers.json")}
}

var _ System = (*CCU)(nil)

// Interfaces implements System.
func (c *CCU) Interfaces(ctx context.Context) ([]litesource.Interface, error) {
	return c.Src.Interfaces(ctx)
}

// Devices implements System.
func (c *CCU) Devices(ctx context.Context, iface string) ([]litesource.DeviceDescription, error) {
	return c.Src.Devices(ctx, iface)
}

// Call implements System: the kit's XML-RPC call to the interface process (Double parameters
// as doubles, the answer as JSON).
func (c *CCU) Call(ctx context.Context, iface, method string, params ...any) (json.RawMessage, error) {
	return c.Src.Call(ctx, iface, method, params...)
}

// Subscribe implements System over the callback server.
func (c *CCU) Subscribe(ctx context.Context, f litesource.Filter, since string) <-chan litesource.Message {
	return c.Src.Subscribe(ctx, f, since)
}

// Descriptions implements System.
func (c *CCU) Descriptions(ctx context.Context, iface string, addresses []string) (map[string]string, litesource.ParamsetCache, error) {
	return c.Src.Descriptions(ctx, iface, addresses)
}

// Objects implements System: the names and rooms from ReGa, the markers from the addon's file.
func (c *CCU) Objects(ctx context.Context, lang string) (Objects, error) {
	names, err := c.Src.Names(ctx)
	if err != nil {
		return nil, err
	}
	markers, err := c.loadMarkers()
	if err != nil {
		return nil, err
	}
	out := make(Objects, len(names.Objects))
	for ref, o := range names.Objects {
		i := strings.IndexByte(ref, '.')
		if i < 0 {
			continue
		}
		obj := Object{Name: o.Name, Rooms: names.RoomNames(ref[:i], ref[i+1:], lang)}
		// ReGa's default name is the address: the addon's "unnamed" rule covers it
		if m, ok := markers[ref]; ok {
			b, _ := json.Marshal(m)
			obj.Meta = map[string]json.RawMessage{Namespace: b}
		}
		out[ref] = obj
	}
	for ref, m := range markers {
		if _, ok := out[ref]; !ok {
			b, _ := json.Marshal(m)
			out[ref] = Object{Meta: map[string]json.RawMessage{Namespace: b}}
		}
	}
	return out, nil
}

// SetObject implements System: the marker into the addon's file, the name through ReGa.
func (c *CCU) SetObject(ctx context.Context, iface, address string, name *string, marker *Marker) error {
	if name != nil {
		if err := c.rename(ctx, iface, address, *name); err != nil {
			return err
		}
	}
	markers, err := c.loadMarkers()
	if err != nil {
		return err
	}
	ref := iface + "." + address
	if marker == nil {
		delete(markers, ref)
	} else {
		markers[ref] = *marker
	}
	return c.saveMarkers(markers)
}

// rename sets a channel's name in ReGa: the channel object is found by interface and address
// ("BidCos-RF.BidCoS-RF:48" is how ReGa names the object before the user renames it, and
// dom.GetObject by that name finds it; a renamed one is looked up through the device's channels).
func (c *CCU) rename(ctx context.Context, iface, address, name string) error {
	dev := DeviceOf(address)
	script := fmt.Sprintf(`string sIf = %s; string sAddr = %s; string sName = %s;
object oCh = dom.GetObject(sIf # "." # sAddr);
if (!oCh) {
  string sDevId;
  foreach(sDevId, root.Devices().EnumUsedIDs()) {
    object oDev = dom.GetObject(sDevId);
    if (oDev && (oDev.Address() == %s)) {
      string sChnId;
      foreach(sChnId, oDev.Channels().EnumUsedIDs()) {
        object oC = dom.GetObject(sChnId);
        if (oC && (oC.Address() == sAddr)) { oCh = oC; }
      }
    }
  }
}
if (oCh) { oCh.Name(sName); WriteLine("ok"); } else { WriteLine("missing"); }
`, regaString(iface), regaString(address), regaString(name), regaString(dev))
	out, err := c.Src.Rega(ctx, script)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "ok") {
		return fmt.Errorf("ReGa has no channel %s %s", iface, address)
	}
	return nil
}

// regaString quotes a string for HM-Script.
func regaString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func (c *CCU) loadMarkers() (map[string]Marker, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return copyMarkers(c.markers), nil
	}
	c.markers = map[string]Marker{}
	b, err := os.ReadFile(c.MarkersFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(b, &c.markers); err != nil {
			return nil, fmt.Errorf("%s: %w", c.MarkersFile, err)
		}
	}
	c.loaded = true
	return copyMarkers(c.markers), nil
}

func copyMarkers(m map[string]Marker) map[string]Marker {
	out := make(map[string]Marker, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (c *CCU) saveMarkers(m map[string]Marker) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]Marker, len(m))
	for _, k := range keys {
		ordered[k] = m[k]
	}
	b, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.MarkersFile), 0o755); err != nil {
		return err
	}
	tmp := c.MarkersFile + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.MarkersFile); err != nil {
		os.Remove(tmp)
		return err
	}
	c.mu.Lock()
	c.markers = copyMarkers(m)
	c.loaded = true
	c.mu.Unlock()
	return nil
}
