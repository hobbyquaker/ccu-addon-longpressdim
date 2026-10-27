// Package keys is the inventory of an interface's virtual keys: which are linked, named or
// marked, which are free, which hold links the addon did not create ("present, not managed").
package keys

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/system"
)

// Key is one virtual key and what the system says about it.
type Key struct {
	N       int    `json:"n"`
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	// Links are the links the key is the sender or receiver of.
	Links []system.Link `json:"links,omitempty"`
	// Marker is the addon's own, when the key belongs to a binding.
	Marker *system.Marker `json:"marker,omitempty"`
	// Foreign: another consumer's namespace is on the key's metadata.
	Foreign bool `json:"foreign,omitempty"`
	// Free: no link, no name, no marker, nothing foreign.
	Free bool `json:"free"`
}

// Unnamed tells whether a channel's name is none: empty, or the system's default - the address
// itself, or the CCU's "<type> <address>" (openccu-lite's metadata holds an object for every
// virtual key, named after its address, until the user names it).
func Unnamed(address, name string) bool {
	return name == "" || name == address || strings.HasSuffix(name, " "+address)
}

// Inventory reads the 50 keys of an interface. links and objects are passed in so that one
// read serves every interface and every key.
func Inventory(iface string, links []system.Link, objects system.Objects) []Key {
	dev := profiles.KeyDevice(iface)
	if dev == "" {
		return nil
	}
	byKey := map[string][]system.Link{}
	for _, l := range links {
		if strings.HasPrefix(l.Sender, dev+":") {
			byKey[l.Sender] = append(byKey[l.Sender], l)
		}
		if strings.HasPrefix(l.Receiver, dev+":") && l.Receiver != l.Sender {
			byKey[l.Receiver] = append(byKey[l.Receiver], l)
		}
	}
	out := make([]Key, 0, profiles.Keys)
	for n := 1; n <= profiles.Keys; n++ {
		addr := profiles.KeyAddress(iface, n)
		o := objects.Get(iface, addr)
		k := Key{N: n, Address: addr, Name: o.Name, Links: byKey[addr], Marker: o.Marker(), Foreign: o.Foreign()}
		k.Free = len(k.Links) == 0 && Unnamed(addr, k.Name) && k.Marker == nil && !k.Foreign
		out = append(out, k)
	}
	return out
}

// Free answers the free keys' numbers from 50 downwards (the WebUI and users take the low ones).
func Free(inv []Key) []int {
	var out []int
	for i := len(inv) - 1; i >= 0; i-- {
		if inv[i].Free {
			out = append(out, inv[i].N)
		}
	}
	return out
}

// Pick answers n free keys from the top, or an error naming how many there are.
func Pick(inv []Key, n int) ([]string, error) {
	free := Free(inv)
	if len(free) < n {
		return nil, fmt.Errorf("only %d free virtual keys, %d needed", len(free), n)
	}
	out := make([]string, n)
	for i := range out {
		out[i] = inv[free[i]-1].Address
	}
	return out, nil
}

// Find answers a key by address.
func Find(inv []Key, address string) (Key, bool) {
	for _, k := range inv {
		if k.Address == address {
			return k, true
		}
	}
	return Key{}, false
}

// Channel is a channel of the system the addon may link to (a target) or relay from (a sender).
type Channel struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Type      string `json:"type"`
	// Device is the device type (HmIP-PDT, HMW-LC-Dim1L-DR, …).
	Device string          `json:"device"`
	Family profiles.Family `json:"family,omitempty"`
	Kind   profiles.Kind   `json:"kind,omitempty"`
	Name   string          `json:"name,omitempty"`
	Rooms  []string        `json:"rooms,omitempty"`
}

// Targets lists the dimmer and blind channels of an interface.
func Targets(iface string, devs []litesource.DeviceDescription, objects system.Objects) []Channel {
	types := deviceTypes(devs)
	var out []Channel
	for _, d := range devs {
		f, ok := profiles.FamilyOf(iface, d.Type)
		if !ok || d.Parent == "" {
			continue
		}
		o := objects.Get(iface, d.Address)
		out = append(out, Channel{Interface: iface, Address: d.Address, Type: d.Type, Device: types[d.Parent], Family: f, Kind: f.Kind(), Name: o.Name, Rooms: o.Rooms})
	}
	sortChannels(out)
	return out
}

// Senders lists the key channels of an interface: every channel whose VALUES carry PRESS_SHORT
// or PRESS_LONG, the interface's own virtual remote left out (its keys are never senders).
func Senders(ctx context.Context, sys system.System, iface string, devs []litesource.DeviceDescription, objects system.Objects) ([]Channel, error) {
	types := deviceTypes(devs)
	vdev := profiles.KeyDevice(iface)
	var addrs []string
	for _, d := range devs {
		if d.Parent == "" || d.Parent == vdev || strings.HasSuffix(d.Address, ":0") {
			continue
		}
		addrs = append(addrs, d.Address)
	}
	if len(addrs) == 0 {
		return nil, nil
	}
	keys, sets, err := sys.Descriptions(ctx, iface, addrs)
	if err != nil {
		return nil, err
	}
	var out []Channel
	for _, d := range devs {
		k, ok := keys[d.Address]
		if !ok {
			continue
		}
		ps := sets[k]
		if _, short := ps["PRESS_SHORT"]; !short {
			if _, long := ps["PRESS_LONG"]; !long {
				continue
			}
		}
		o := objects.Get(iface, d.Address)
		out = append(out, Channel{Interface: iface, Address: d.Address, Type: d.Type, Device: types[d.Parent], Name: o.Name, Rooms: o.Rooms})
	}
	sortChannels(out)
	return out, nil
}

func deviceTypes(devs []litesource.DeviceDescription) map[string]string {
	types := map[string]string{}
	for _, d := range devs {
		if d.Parent == "" {
			types[d.Address] = d.Type
		}
	}
	return types
}

func sortChannels(cs []Channel) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Name != cs[j].Name {
			if cs[i].Name == "" {
				return false
			}
			if cs[j].Name == "" {
				return true
			}
			return cs[i].Name < cs[j].Name
		}
		return cs[i].Address < cs[j].Address
	})
}

// ForeignLink is a link from a virtual key to a dimmer or blind that no binding owns: shown as
// "present, not managed", adoptable.
type ForeignLink struct {
	Interface string `json:"interface"`
	Key       string `json:"key"`
	KeyName   string `json:"key_name,omitempty"`
	Receiver  Channel
	Link      system.Link `json:"link"`
	// Profile is what the LINK paramset says the key does; nil when it could not be read.
	Profile *profiles.Classification `json:"profile,omitempty"`
	Error   string                   `json:"error,omitempty"`
}

// Foreign finds the links from virtual keys to targets that are not owned by a binding (owned
// names the keys that are: <iface>|<key>) and classifies each by its paramset.
func Foreign(ctx context.Context, sys system.System, iface string, links []system.Link, targets []Channel, objects system.Objects, owned map[string]bool) []ForeignLink {
	dev := profiles.KeyDevice(iface)
	byAddr := map[string]Channel{}
	for _, t := range targets {
		byAddr[t.Address] = t
	}
	var out []ForeignLink
	for _, l := range links {
		if !strings.HasPrefix(l.Sender, dev+":") || owned[iface+"|"+l.Sender] {
			continue
		}
		t, ok := byAddr[l.Receiver]
		if !ok {
			continue
		}
		fl := ForeignLink{Interface: iface, Key: l.Sender, KeyName: objects.Get(iface, l.Sender).Name, Receiver: t, Link: l}
		ps, err := system.LinkParamset(ctx, sys, iface, l.Receiver, l.Sender)
		if err != nil {
			fl.Error = err.Error()
		} else {
			c := profiles.Classify(t.Family, ps)
			fl.Profile = &c
		}
		out = append(out, fl)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Receiver.Address != out[j].Receiver.Address {
			return out[i].Receiver.Address < out[j].Receiver.Address
		}
		return keyN(out[i].Key) < keyN(out[j].Key)
	})
	return out
}

func keyN(addr string) int {
	i := strings.LastIndexByte(addr, ':')
	n := 0
	fmt.Sscanf(addr[i+1:], "%d", &n)
	return n
}
