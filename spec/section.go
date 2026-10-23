package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

// Section is a manifest section written as a YAML mapping whose declaration
// order is preserved.
//
// Order matters and a Go map would lose it: Tasks run in "the first Task in
// declaration order whose needs are satisfied" order, and Checks run in
// declaration order. Preserving it here is what lets the planner make Task
// order a property of the Plan rather than of the run.
type Section[T any] struct {
	names  []string
	byName map[string]T
}

// NewSection builds a Section in the given order. Intended for tests and for
// callers constructing an Environment programmatically.
func NewSection[T any](names []string, byName map[string]T) Section[T] {
	s := Section[T]{}
	for _, n := range names {
		if v, ok := byName[n]; ok {
			s.Set(n, v)
		}
	}
	return s
}

// Names returns the keys in declaration order.
func (s Section[T]) Names() []string { return s.names }

// Get returns the entry and whether it is present.
func (s Section[T]) Get(name string) (T, bool) {
	v, ok := s.byName[name]
	return v, ok
}

// Len returns the number of entries.
func (s Section[T]) Len() int { return len(s.names) }

// Set adds or replaces an entry, appending to the declaration order when the
// key is new.
func (s *Section[T]) Set(name string, v T) {
	if s.byName == nil {
		s.byName = make(map[string]T)
	}
	if _, exists := s.byName[name]; !exists {
		s.names = append(s.names, name)
	}
	s.byName[name] = v
}

// UnmarshalYAML reads a mapping, keeping its key order.
//
// Values are decoded leniently here on purpose: unknown fields are reported
// with full field paths by the separate schema walk in decode.go, which gives
// better errors than yaml's own KnownFields can.
func (s *Section[T]) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a mapping of names to entries")
	}
	s.names = nil
	s.byName = make(map[string]T, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		var key string
		if err := n.Content[i].Decode(&key); err != nil {
			return err
		}
		if _, dup := s.byName[key]; dup {
			return fmt.Errorf("duplicate name %q", key)
		}
		var val T
		if err := n.Content[i+1].Decode(&val); err != nil {
			return err
		}
		s.names = append(s.names, key)
		s.byName[key] = val
	}
	return nil
}

// MarshalJSON writes the entries in declaration order, so that a reported
// Environment reads the way the author wrote it.
func (s Section[T]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, name := range s.names {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(s.byName[name])
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// elemType lets the schema walk in decode.go recurse into a Section's entries
// without knowing which Section it is looking at.
func (s Section[T]) elemType() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// elemTyper is implemented by Section for any entry type.
type elemTyper interface {
	elemType() reflect.Type
}
