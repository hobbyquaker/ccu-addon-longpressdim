// Package store keeps the bindings (var/bindings.json) and the settings (etc/longpressdim.json)
// as JSON files, each written atomically. The bindings file repeats what the system's metadata
// marks on the keys; the metadata is the cross-check at start that nothing took a key in between.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/model"
)

// Format is the bindings file's format number.
const Format = 1

type file struct {
	Format   int             `json:"format"`
	Bindings []model.Binding `json:"bindings"`
}

// Store is the bindings file in memory.
type Store struct {
	path string
	mu   sync.Mutex
	list map[string]model.Binding
}

// Open reads var/bindings.json of the addon directory; a missing file is an empty store, a
// damaged one an error (it is never overwritten by accident).
func Open(addonDir string) (*Store, error) {
	s := &Store{path: filepath.Join(addonDir, "var", "bindings.json"), list: map[string]model.Binding{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	if f.Format > Format {
		return nil, fmt.Errorf("%s: format %d is newer than this addon's %d", s.path, f.Format, Format)
	}
	for _, x := range f.Bindings {
		s.list[x.ID] = x
	}
	return s, nil
}

// Path answers the file's path.
func (s *Store) Path() string { return s.path }

// List answers every binding, sorted by id.
func (s *Store) List() []model.Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sorted()
}

// clone copies a binding so that no caller shares its slices with the store (a sender's usage
// written into the store must not appear in the caller's draft, and the other way round).
func clone(b model.Binding) model.Binding {
	if b.Senders != nil {
		senders := make([]model.Sender, len(b.Senders))
		for i, snd := range b.Senders {
			if snd.Usage != nil {
				u := *snd.Usage
				snd.Usage = &u
			}
			senders[i] = snd
		}
		b.Senders = senders
	}
	return b
}

func (s *Store) sorted() []model.Binding {
	out := make([]model.Binding, 0, len(s.list))
	for _, b := range s.list {
		out = append(out, clone(b))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get answers one binding.
func (s *Store) Get(id string) (model.Binding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.list[id]
	return clone(b), ok
}

// Put stores a binding (new or changed) and writes the file.
func (s *Store) Put(b model.Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.Created.IsZero() {
		b.Created = time.Now()
	}
	b.Updated = time.Now()
	b = clone(b)
	old, had := s.list[b.ID]
	s.list[b.ID] = b
	if err := s.write(); err != nil {
		if had {
			s.list[b.ID] = old
		} else {
			delete(s.list, b.ID)
		}
		return err
	}
	return nil
}

// Delete removes a binding and writes the file; an unknown id is no error.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.list[id]
	if !had {
		return nil
	}
	delete(s.list, id)
	if err := s.write(); err != nil {
		s.list[id] = old
		return err
	}
	return nil
}

// KeyOwner answers the binding that holds a key, "" when none.
func (s *Store) KeyOwner(iface, key string) (id, role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.list {
		if b.Target.Interface != iface {
			continue
		}
		if b.Up == key {
			return b.ID, "up"
		}
		if b.Down == key {
			return b.ID, "down"
		}
	}
	return "", ""
}

func (s *Store) write() error {
	b, err := json.MarshalIndent(file{Format: Format, Bindings: s.sorted()}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, append(b, '\n'))
}

// writeAtomic writes through a temporary file in the same directory and renames it over the
// target, so a crash leaves either the old file or the new one.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if werr != nil || serr != nil || cerr != nil {
		os.Remove(name)
		return errors.Join(werr, serr, cerr)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Settings is etc/longpressdim.json.
type Settings struct {
	path string
	mu   sync.Mutex
	cur  model.Settings
}

// OpenSettings reads the settings; a missing or damaged file gives the defaults (and is written
// over at the next save).
func OpenSettings(addonDir string) *Settings {
	s := &Settings{path: filepath.Join(addonDir, "etc", "longpressdim.json"), cur: model.DefaultSettings()}
	if b, err := os.ReadFile(s.path); err == nil {
		var v model.Settings
		if json.Unmarshal(b, &v) == nil {
			s.cur = v.Normalize()
		}
	}
	return s
}

// Get answers the settings.
func (s *Settings) Get() model.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Set stores and writes the settings.
func (s *Settings) Set(v model.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v = v.Normalize()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(s.path, append(b, '\n')); err != nil {
		return err
	}
	s.cur = v
	return nil
}
