// Package links plans and applies the writes a binding needs: the links from its two virtual
// keys to the actuator, the receiver-side LINK parameters that differ from what the actuator
// has, the keys' names and markers - and the reverse for a removal. A plan is computed first and
// shown as a dry run; Apply executes it step by step, reads the paramsets back and reports what
// still differs. An HmIP write that times out is pending on the device, not failed.
package links

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/keys"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/profiles"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/system"
)

// Step kinds.
const (
	AddLink     = "addLink"
	PutParamset = "putParamset"
	RemoveLink  = "removeLink"
	SetMeta     = "setMeta"
)

// Step is one write of a plan.
type Step struct {
	Kind      string `json:"kind"`
	Interface string `json:"interface"`
	Key       string `json:"key"`
	Receiver  string `json:"receiver,omitempty"`
	Role      string `json:"role,omitempty"`
	// Params are the parameters a putParamset writes (the diff); for an addLink the full set
	// the key's role wants, written after the link exists (the diff is known only then).
	Params map[string]any `json:"params,omitempty"`
	// Missing names wanted parameters the receiver's paramset lacks; they are not written.
	Missing []string `json:"missing,omitempty"`
	// Name and Marker of a setMeta step; a nil Marker removes it, a nil Name leaves the name.
	Name   *string        `json:"name,omitempty"`
	Marker *system.Marker `json:"marker,omitempty"`
	// Note says why the step is there or why it is empty.
	Note string `json:"note,omitempty"`
	// Unchanged: the key already has this name and marker; Apply skips the step.
	Unchanged bool `json:"unchanged,omitempty"`
}

// Plan is what Apply will do.
type Plan struct {
	Binding  model.Binding `json:"binding"`
	Steps    []Step        `json:"steps"`
	Warnings []string      `json:"warnings,omitempty"`
}

// Writes tells whether the plan changes anything on the system.
func (p *Plan) Writes() bool {
	for _, s := range p.Steps {
		switch {
		case s.Unchanged:
		case s.Kind == PutParamset && len(s.Params) == 0:
		case s.Kind == SetMeta && s.Marker == nil && s.Name == nil:
		default:
			return true
		}
	}
	return false
}

// Result of an applied plan.
type Result struct {
	Plan *Plan `json:"plan"`
	// Steps are the outcomes, one per plan step, in order.
	Steps []StepResult `json:"steps"`
	// Pending: the device has not taken a write yet (CONFIG_PENDING, or a TIMEOUT on HmIP);
	// the paramset is written again once the link exists and the device answers.
	Pending bool `json:"pending"`
	// Differs lists what the read-back still shows differently, per key.
	Differs map[string]map[string]any `json:"differs,omitempty"`
	// Failed is the first error that stopped the plan, "" when it ran through.
	Failed string `json:"failed,omitempty"`
}

// StepResult is one step's outcome.
type StepResult struct {
	Step   Step   `json:"step"`
	State  string `json:"state"` // done, pending, skipped, failed
	Detail string `json:"detail,omitempty"`
}

// Name is the name the addon gives a key: "<target name> ▲" / "… ▼" (the user may rename it;
// the marker, not the name, is the ownership).
func Name(targetName, direction string) string {
	if targetName == "" {
		targetName = "longpressdim"
	}
	if direction == "up" {
		return targetName + " ▲"
	}
	return targetName + " ▼"
}

// LinkName is the name a link gets where the process keeps one (rfd, hs485d).
func LinkName(b model.Binding, direction string) string {
	return "longpressdim:" + b.ID + ":" + direction
}

// Role answers the key's profile role for a direction.
func Role(b model.Binding, direction string) profiles.Role {
	if direction == "up" {
		return b.UpRole()
	}
	return b.DownRole()
}

// PlanApply computes what a new or changed binding needs: for each key an addLink when the link
// is missing, the parameter diff when it exists, and the metadata. targetName is the actuator's
// name for the keys' names; keyNames are the keys' current names (an adopted key keeps its own).
func PlanApply(ctx context.Context, sys system.System, b model.Binding, links []system.Link, objects system.Objects) (*Plan, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	p := &Plan{Binding: b}
	targetName := objects.Get(b.Target.Interface, b.Target.Address).Name
	for _, dir := range []string{"up", "down"} {
		key := b.Key(dir)
		role := Role(b, dir)
		want, err := profiles.Params(b.Target.Family, role, b.Options)
		if err != nil {
			return nil, err
		}
		if !hasLink(links, key, b.Target.Address) {
			p.Steps = append(p.Steps, Step{Kind: AddLink, Interface: b.Target.Interface, Key: key, Receiver: b.Target.Address, Role: string(role), Params: want, Note: "the link does not exist yet; the parameters are written after it does"})
		} else {
			cur, err := system.LinkParamset(ctx, sys, b.Target.Interface, b.Target.Address, key)
			if err != nil {
				return nil, fmt.Errorf("reading the link %s -> %s: %w", key, b.Target.Address, err)
			}
			diff, missing := profiles.Diff(want, cur)
			s := Step{Kind: PutParamset, Interface: b.Target.Interface, Key: key, Receiver: b.Target.Address, Role: string(role), Params: diff, Missing: missing}
			if len(diff) == 0 {
				s.Note = "the link already has the profile"
			}
			if len(cur) == 0 {
				s.Note = "the receiver answered an empty paramset for this link"
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s -> %s: empty link paramset; nothing can be compared", key, b.Target.Address))
			}
			p.Steps = append(p.Steps, s)
		}
		o := objects.Get(b.Target.Interface, key)
		marker := &system.Marker{Binding: b.ID, Role: dir}
		meta := Step{Kind: SetMeta, Interface: b.Target.Interface, Key: key, Role: string(role), Marker: marker}
		if keys.Unnamed(key, o.Name) || strings.HasSuffix(o.Name, " ▲") || strings.HasSuffix(o.Name, " ▼") {
			n := Name(targetName, dir)
			if o.Name != n {
				meta.Name = &n
			}
		}
		if m := o.Marker(); m != nil && m.Binding != b.ID {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s is marked for the binding %s", key, m.Binding))
		}
		if o.Foreign() {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s carries another consumer's metadata", key))
		}
		if m := o.Marker(); m != nil && *m == *marker && meta.Name == nil {
			meta.Unchanged = true
			meta.Note = "marked already"
		}
		for _, l := range linksOf(links, key) {
			if l.Receiver != b.Target.Address || l.Sender != key {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s has another link: %s -> %s", key, l.Sender, l.Receiver))
			}
		}
		p.Steps = append(p.Steps, meta)
	}
	return p, nil
}

// PlanRemove computes a binding's removal: removeLink for both keys where the link exists, the
// marker removed, the name set back to the system's default (the address: the metadata store
// takes no empty name) when it is the addon's own. forget leaves the links alone
// (an adopted binding, or the user's choice) and only unmarks the keys.
func PlanRemove(b model.Binding, links []system.Link, objects system.Objects, forget bool) *Plan {
	p := &Plan{Binding: b}
	for _, dir := range []string{"up", "down"} {
		key := b.Key(dir)
		if !forget {
			if hasLink(links, key, b.Target.Address) {
				p.Steps = append(p.Steps, Step{Kind: RemoveLink, Interface: b.Target.Interface, Key: key, Receiver: b.Target.Address, Role: dir})
			} else {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s -> %s: no link to remove", key, b.Target.Address))
			}
		}
		o := objects.Get(b.Target.Interface, key)
		meta := Step{Kind: SetMeta, Interface: b.Target.Interface, Key: key, Role: dir}
		if strings.HasSuffix(o.Name, " ▲") || strings.HasSuffix(o.Name, " ▼") {
			def := key
			meta.Name = &def
		}
		if m := o.Marker(); m != nil && m.Binding != b.ID {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s is marked for the binding %s; its marker stays", key, m.Binding))
			continue
		}
		p.Steps = append(p.Steps, meta)
	}
	return p
}

// Apply executes a plan. It stops at the first failure that is not a pending write; what was
// done stays done (the result says what).
func Apply(ctx context.Context, sys system.System, p *Plan) *Result {
	r := &Result{Plan: p, Differs: map[string]map[string]any{}}
	b := p.Binding
	for _, s := range p.Steps {
		sr := StepResult{Step: s, State: "done"}
		switch s.Kind {
		case AddLink:
			err := system.AddLink(ctx, sys, s.Interface, s.Key, s.Receiver, LinkName(b, s.Role), "ccu-addon-longpressdim")
			if err != nil && system.IsTimeout(err) {
				links, lerr := system.Links(ctx, sys, s.Interface)
				if lerr == nil && hasLink(links, s.Key, s.Receiver) {
					sr.State, sr.Detail = "pending", "the process answered TIMEOUT; the link exists and waits for the device"
					r.Pending = true
					err = nil
				}
			}
			if err != nil {
				sr.State, sr.Detail = "failed", err.Error()
				r.Steps = append(r.Steps, sr)
				r.Failed = fmt.Sprintf("addLink %s -> %s: %v", s.Key, s.Receiver, err)
				return r
			}
			r.Steps = append(r.Steps, sr)
			// now the parameters: read what the link got, write the difference
			cur, err := system.LinkParamset(ctx, sys, s.Interface, s.Receiver, s.Key)
			if err != nil {
				r.Steps = append(r.Steps, StepResult{Step: Step{Kind: PutParamset, Interface: s.Interface, Key: s.Key, Receiver: s.Receiver, Role: s.Role}, State: "failed", Detail: err.Error()})
				r.Failed = fmt.Sprintf("reading the new link %s -> %s: %v", s.Key, s.Receiver, err)
				return r
			}
			diff, missing := profiles.Diff(s.Params, cur)
			ps := Step{Kind: PutParamset, Interface: s.Interface, Key: s.Key, Receiver: s.Receiver, Role: s.Role, Params: diff, Missing: missing}
			if res, stop := putStep(ctx, sys, ps, r); stop {
				return r
			} else {
				r.Steps = append(r.Steps, res)
			}
		case PutParamset:
			if res, stop := putStep(ctx, sys, s, r); stop {
				return r
			} else {
				r.Steps = append(r.Steps, res)
			}
		case RemoveLink:
			if err := system.RemoveLink(ctx, sys, s.Interface, s.Key, s.Receiver); err != nil {
				if system.IsTimeout(err) {
					links, lerr := system.Links(ctx, sys, s.Interface)
					if lerr == nil && !hasLink(links, s.Key, s.Receiver) {
						sr.State, sr.Detail = "pending", "the process answered TIMEOUT; the link is gone and the device learns it later"
						r.Pending = true
						r.Steps = append(r.Steps, sr)
						continue
					}
				}
				sr.State, sr.Detail = "failed", err.Error()
				r.Steps = append(r.Steps, sr)
				r.Failed = fmt.Sprintf("removeLink %s -> %s: %v", s.Key, s.Receiver, err)
				return r
			}
			r.Steps = append(r.Steps, sr)
		case SetMeta:
			if s.Unchanged {
				sr.State, sr.Detail = "skipped", s.Note
				r.Steps = append(r.Steps, sr)
				continue
			}
			if err := sys.SetObject(ctx, s.Interface, s.Key, s.Name, s.Marker); err != nil {
				sr.State, sr.Detail = "failed", err.Error()
				r.Steps = append(r.Steps, sr)
				r.Failed = fmt.Sprintf("metadata of %s: %v", s.Key, err)
				return r
			}
			r.Steps = append(r.Steps, sr)
		default:
			sr.State, sr.Detail = "skipped", "unknown step"
			r.Steps = append(r.Steps, sr)
		}
	}
	return r
}

// putStep writes a paramset diff and reads it back; an empty diff is skipped. It answers the
// outcome and whether the plan must stop.
func putStep(ctx context.Context, sys system.System, s Step, r *Result) (StepResult, bool) {
	sr := StepResult{Step: s, State: "done"}
	if len(s.Params) == 0 {
		sr.State, sr.Detail = "skipped", "nothing differs"
		return sr, false
	}
	err := system.PutLinkParamset(ctx, sys, s.Interface, s.Receiver, s.Key, s.Params)
	if err != nil && system.IsTimeout(err) {
		sr.State, sr.Detail = "pending", "the process answered TIMEOUT; the parameters wait for the device"
		r.Pending = true
		err = nil
	}
	if err != nil {
		sr.State, sr.Detail = "failed", err.Error()
		r.Failed = fmt.Sprintf("putParamset %s -> %s: %v", s.Key, s.Receiver, err)
		return sr, true
	}
	cur, rerr := system.LinkParamset(ctx, sys, s.Interface, s.Receiver, s.Key)
	if rerr != nil {
		sr.Detail = strings.TrimSpace(sr.Detail + "; read-back failed: " + rerr.Error())
		return sr, false
	}
	if diff, _ := profiles.Diff(s.Params, cur); len(diff) > 0 {
		r.Differs[s.Key] = diff
		if sr.State == "done" {
			sr.Detail = fmt.Sprintf("read back differently: %s", keysOf(diff))
		}
	}
	return sr, false
}

// Verify compares a binding's links with its profile: both links present, the parameters as
// planned. It answers what differs per key, the keys whose link is missing, and whether the
// device is still pending.
func Verify(ctx context.Context, sys system.System, b model.Binding, links []system.Link) (differs map[string]map[string]any, missing []string, err error) {
	differs = map[string]map[string]any{}
	for _, dir := range []string{"up", "down"} {
		key := b.Key(dir)
		if !hasLink(links, key, b.Target.Address) {
			missing = append(missing, key)
			continue
		}
		want, perr := profiles.Params(b.Target.Family, Role(b, dir), b.Options)
		if perr != nil {
			return nil, nil, perr
		}
		cur, rerr := system.LinkParamset(ctx, sys, b.Target.Interface, b.Target.Address, key)
		if rerr != nil {
			return nil, nil, rerr
		}
		if diff, _ := profiles.Diff(want, cur); len(diff) > 0 {
			differs[key] = diff
		}
	}
	return differs, missing, nil
}

// ErrNoLink says a link the binding needs does not exist.
var ErrNoLink = errors.New("link missing")

func hasLink(links []system.Link, key, receiver string) bool {
	for _, l := range links {
		if l.Sender == key && l.Receiver == receiver {
			return true
		}
	}
	return false
}

func linksOf(links []system.Link, key string) []system.Link {
	var out []system.Link
	for _, l := range links {
		if l.Sender == key || l.Receiver == key {
			out = append(out, l)
		}
	}
	return out
}

func keysOf(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ", ")
}

// Pretty renders a diff for a log line.
func Pretty(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
