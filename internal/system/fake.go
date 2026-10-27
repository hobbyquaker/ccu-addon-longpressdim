package system

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// Fake is an in-memory system for tests: devices, links and link paramsets per interface,
// the metadata, an event feed, and a record of every RPC call. Methods of the interface
// processes it answers: getLinks, getParamset (LINK and channel 0 VALUES), putParamset, addLink,
// removeLink, setValue. Errors can be injected per method.
type Fake struct {
	mu sync.Mutex
	// Ifaces are the interfaces, in order.
	Ifaces []litesource.Interface
	// Devs holds the device descriptions per interface.
	Devs map[string][]litesource.DeviceDescription
	// LinksOf holds the links per interface.
	LinksOf map[string][]Link
	// Paramsets holds the receiver-side LINK paramsets, keyed iface|receiver|key.
	Paramsets map[string]map[string]json.RawMessage
	// Values holds channel VALUES, keyed iface|address (CONFIG_PENDING lives in <device>:0).
	Values map[string]map[string]json.RawMessage
	// Meta is the metadata, keyed iface.address.
	Meta Objects
	// Desc holds the VALUES descriptions per interface and channel type key.
	Desc map[string]litesource.ParamsetCache
	// DescKeys maps iface|address to its description key.
	DescKeys map[string]string
	// Calls records every Call as "iface method p1 p2 …".
	Calls []string
	// Fail injects an error for a method ("addLink" → err); AddLinkTimeout makes addLink answer
	// a TIMEOUT fault and still create the link (hmipserver's behaviour).
	Fail           map[string]error
	AddLinkTimeout bool
	// DefaultParamset is what a new link's paramset starts with, per family key (receiver type);
	// nil: an empty paramset.
	DefaultParamset func(iface, receiver string) map[string]json.RawMessage
	// Usages records reportValueUsage, keyed iface|address|value -> count; UsagePending makes
	// the call answer false (the device must be reached first).
	Usages       map[string]int
	UsagePending bool
	// Events is the feed Subscribe reads from; the test closes it or cancels the context.
	Events chan litesource.Message
	// SubscribeFilter is the filter of the last Subscribe.
	SubscribeFilter litesource.Filter
	Subscribed      int
}

// NewFake makes an empty fake with the three interfaces.
func NewFake() *Fake {
	return &Fake{
		Ifaces: []litesource.Interface{{Name: "BidCos-RF", Running: true}, {Name: "BidCos-Wired", Running: true}, {Name: "HmIP-RF", Running: true}},
		Devs:   map[string][]litesource.DeviceDescription{}, LinksOf: map[string][]Link{},
		Paramsets: map[string]map[string]json.RawMessage{}, Values: map[string]map[string]json.RawMessage{},
		Meta: Objects{}, Desc: map[string]litesource.ParamsetCache{}, DescKeys: map[string]string{},
		Fail: map[string]error{}, Events: make(chan litesource.Message, 64), Usages: map[string]int{},
	}
}

var _ System = (*Fake)(nil)

// AddDevice adds a device with channels of the given types (channel 0 gets MAINTENANCE).
func (f *Fake) AddDevice(iface, address, typ string, channels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kids []string
	for i := range channels {
		kids = append(kids, fmt.Sprintf("%s:%d", address, i+1))
	}
	f.Devs[iface] = append(f.Devs[iface], litesource.DeviceDescription{Address: address, Type: typ, Children: append([]string{address + ":0"}, kids...), Interface: iface})
	f.Devs[iface] = append(f.Devs[iface], litesource.DeviceDescription{Address: address + ":0", Type: "MAINTENANCE", Parent: address, Interface: iface})
	for i, t := range channels {
		f.Devs[iface] = append(f.Devs[iface], litesource.DeviceDescription{Address: fmt.Sprintf("%s:%d", address, i+1), Type: t, Parent: address, Interface: iface})
	}
	f.Values[iface+"|"+address+":0"] = map[string]json.RawMessage{"CONFIG_PENDING": json.RawMessage("false"), "UNREACH": json.RawMessage("false")}
}

// AddVirtualRemote adds the interface's 50-key virtual remote.
func (f *Fake) AddVirtualRemote(iface string) {
	dev, typ, ch := "BidCoS-RF", "HM-RCV-50", "VIRTUAL_KEY"
	switch iface {
	case "BidCos-Wired":
		dev, typ = "BidCoS-Wir", "HMW-RCV-50"
	case "HmIP-RF":
		dev, typ, ch = "HmIP-RCV-1", "HmIP-RCV-50", "KEY_TRANSCEIVER"
	}
	chs := make([]string, 50)
	for i := range chs {
		chs[i] = ch
	}
	f.AddDevice(iface, dev, typ, chs...)
}

// SetParamset sets a link paramset.
func (f *Fake) SetParamset(iface, receiver, key string, ps map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Paramsets[iface+"|"+receiver+"|"+key] = rawMap(ps)
}

// Paramset answers a link paramset (nil when none).
func (f *Fake) Paramset(iface, receiver, key string) map[string]json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Paramsets[iface+"|"+receiver+"|"+key]
}

// SetPending sets a device's CONFIG_PENDING.
func (f *Fake) SetPending(iface, device string, pending bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.Values[iface+"|"+device+":0"]
	if v == nil {
		v = map[string]json.RawMessage{}
		f.Values[iface+"|"+device+":0"] = v
	}
	v["CONFIG_PENDING"] = json.RawMessage(fmt.Sprint(pending))
}

// CallsOf answers the recorded calls of a method.
func (f *Fake) CallsOf(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Calls {
		if strings.Split(c, " ")[1] == method {
			out = append(out, c)
		}
	}
	return out
}

func rawMap(m map[string]any) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		if d, ok := v.(litesource.Double); ok {
			v = float64(d)
		}
		b, _ := json.Marshal(v)
		out[k] = b
	}
	return out
}

// Interfaces implements System.
func (f *Fake) Interfaces(ctx context.Context) ([]litesource.Interface, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Fail["interfaces"]; err != nil {
		return nil, err
	}
	return append([]litesource.Interface(nil), f.Ifaces...), nil
}

// Devices implements System.
func (f *Fake) Devices(ctx context.Context, iface string) ([]litesource.DeviceDescription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Fail["listDevices"]; err != nil {
		return nil, err
	}
	return append([]litesource.DeviceDescription(nil), f.Devs[iface]...), nil
}

// Call implements System.
func (f *Fake) Call(ctx context.Context, iface, method string, params ...any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := iface + " " + method
	for _, p := range params {
		if m, ok := p.(map[string]any); ok {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var parts []string
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
			}
			rec += " {" + strings.Join(parts, ",") + "}"
		} else {
			rec += fmt.Sprintf(" %v", p)
		}
	}
	f.Calls = append(f.Calls, rec)
	if err := f.Fail[method]; err != nil {
		return nil, err
	}
	str := func(i int) string {
		if i < len(params) {
			s, _ := params[i].(string)
			return s
		}
		return ""
	}
	switch method {
	case "getLinks":
		return json.Marshal(f.LinksOf[iface])
	case "getParamset":
		addr, which := str(0), str(1)
		if which == "VALUES" || which == "MASTER" {
			v := f.Values[iface+"|"+addr]
			if v == nil {
				v = map[string]json.RawMessage{}
			}
			return json.Marshal(v)
		}
		ps := f.Paramsets[iface+"|"+addr+"|"+which]
		if ps == nil {
			ps = map[string]json.RawMessage{}
		}
		return json.Marshal(ps)
	case "putParamset":
		addr, which := str(0), str(1)
		m, _ := params[2].(map[string]any)
		key := iface + "|" + addr + "|" + which
		if which == "VALUES" {
			key = iface + "|" + addr
			if f.Values[key] == nil {
				f.Values[key] = map[string]json.RawMessage{}
			}
			for k, v := range rawMap(m) {
				f.Values[key][k] = v
			}
			return json.RawMessage("\"\""), nil
		}
		if f.Paramsets[key] == nil {
			return nil, fmt.Errorf("rpc -1: Generic error: no link %s -> %s", which, addr)
		}
		for k, v := range rawMap(m) {
			if _, ok := f.Paramsets[key][k]; !ok {
				return nil, fmt.Errorf("rpc -1: Generic error: unknown parameter %s", k)
			}
			f.Paramsets[key][k] = v
		}
		return json.RawMessage("\"\""), nil
	case "addLink":
		sender, receiver := str(0), str(1)
		for _, l := range f.LinksOf[iface] {
			if l.Sender == sender && l.Receiver == receiver {
				return nil, fmt.Errorf("rpc -1: Generic error: link exists")
			}
		}
		f.LinksOf[iface] = append(f.LinksOf[iface], Link{Sender: sender, Receiver: receiver, Name: str(2), Description: str(3)})
		key := iface + "|" + receiver + "|" + sender
		if f.DefaultParamset != nil {
			f.Paramsets[key] = f.DefaultParamset(iface, receiver)
		} else {
			f.Paramsets[key] = map[string]json.RawMessage{}
		}
		if f.AddLinkTimeout {
			f.Values[iface+"|"+DeviceOf(receiver)+":0"]["CONFIG_PENDING"] = json.RawMessage("true")
			return nil, fmt.Errorf("rpc -1: Generic error (TIMEOUT)")
		}
		return json.RawMessage("\"\""), nil
	case "removeLink":
		sender, receiver := str(0), str(1)
		var kept []Link
		found := false
		for _, l := range f.LinksOf[iface] {
			if l.Sender == sender && l.Receiver == receiver {
				found = true
				continue
			}
			kept = append(kept, l)
		}
		if !found {
			return nil, fmt.Errorf("rpc -1: Generic error: no such link")
		}
		f.LinksOf[iface] = kept
		delete(f.Paramsets, iface+"|"+receiver+"|"+sender)
		return json.RawMessage("\"\""), nil
	case "setValue":
		return json.RawMessage("\"\""), nil
	case "reportValueUsage":
		n, _ := params[2].(int)
		f.Usages[iface+"|"+str(0)+"|"+str(1)] = n
		if f.UsagePending {
			return json.RawMessage("false"), nil
		}
		return json.RawMessage("true"), nil
	}
	return nil, fmt.Errorf("fake: unknown method %s", method)
}

// Subscribe implements System over the Events channel.
func (f *Fake) Subscribe(ctx context.Context, fl litesource.Filter, since string) <-chan litesource.Message {
	f.mu.Lock()
	f.SubscribeFilter = fl
	f.Subscribed++
	f.mu.Unlock()
	out := make(chan litesource.Message, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-f.Events:
				if !ok {
					return
				}
				select {
				case out <- m:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// Objects implements System.
func (f *Fake) Objects(ctx context.Context, lang string) (Objects, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Fail["objects"]; err != nil {
		return nil, err
	}
	out := make(Objects, len(f.Meta))
	for k, v := range f.Meta {
		meta := make(map[string]json.RawMessage, len(v.Meta))
		for ns, raw := range v.Meta {
			meta[ns] = raw
		}
		v.Meta = meta
		out[k] = v
	}
	return out, nil
}

// SetObject implements System.
func (f *Fake) SetObject(ctx context.Context, iface, address string, name *string, marker *Marker) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Fail["setObject"]; err != nil {
		return err
	}
	ref := iface + "." + address
	o := f.Meta[ref]
	if o.Meta == nil {
		o.Meta = map[string]json.RawMessage{}
	}
	if name != nil {
		o.Name = *name
	}
	if marker == nil {
		delete(o.Meta, Namespace)
	} else {
		b, _ := json.Marshal(marker)
		o.Meta[Namespace] = b
	}
	f.Meta[ref] = o
	f.Calls = append(f.Calls, fmt.Sprintf("%s setObject %s name=%v marker=%v", iface, address, name != nil, marker != nil))
	return nil
}

// Descriptions implements System.
func (f *Fake) Descriptions(ctx context.Context, iface string, addresses []string) (map[string]string, litesource.ParamsetCache, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := map[string]string{}
	for _, a := range addresses {
		if k, ok := f.DescKeys[iface+"|"+a]; ok {
			keys[a] = k
		}
	}
	return keys, f.Desc[iface], nil
}

// Emit feeds an event into the stream.
func (f *Fake) Emit(iface, address, key string, value any) {
	b, _ := json.Marshal(value)
	f.Events <- litesource.Message{Type: "event", Event: &litesource.Event{Interface: iface, Address: address, Key: key, Value: b}}
}
