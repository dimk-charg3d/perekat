// Package trust remembers paired devices and the certificates they presented.
package trust

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
)

// Entry is one paired device.
type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Cert []byte `json:"cert"`
}

// Store is a file-backed set of paired devices, safe for concurrent use.
type Store struct {
	path    string
	mu      sync.Mutex
	entries map[string]Entry
}

// Open loads the store at path; a missing file is an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Entry
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	for _, e := range list {
		s.entries[e.ID] = e
	}
	return s, nil
}

func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	return e, ok
}

func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	return list
}

// Matches reports whether id is paired and cert is the one it was paired with.
func (s *Store) Matches(id string, cert []byte) bool {
	e, ok := s.Get(id)
	return ok && bytes.Equal(e.Cert, cert)
}

func (s *Store) Add(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[e.ID] = e
	return s.save()
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
	return s.save()
}

func (s *Store) save() error {
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
