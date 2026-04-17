package runtime

import (
	"encoding/json"
	"sort"
)

type SetValue struct {
	items map[string]any
}

func NewSetValue(values []any) *SetValue {
	set := &SetValue{items: make(map[string]any, len(values))}
	for _, value := range values {
		set.items[setKey(value)] = cloneValue(value)
	}
	return set
}

func (s *SetValue) Clone() *SetValue {
	if s == nil {
		return NewSetValue(nil)
	}
	cloned := &SetValue{items: make(map[string]any, len(s.items))}
	for key, value := range s.items {
		cloned.items[key] = cloneValue(value)
	}
	return cloned
}

func (s *SetValue) Len() int {
	if s == nil {
		return 0
	}
	return len(s.items)
}

func (s *SetValue) Add(value any) *SetValue {
	next := s.Clone()
	next.items[setKey(value)] = cloneValue(value)
	return next
}

func (s *SetValue) Remove(value any) *SetValue {
	next := s.Clone()
	delete(next.items, setKey(value))
	return next
}

func (s *SetValue) Has(value any) bool {
	if s == nil {
		return false
	}
	_, ok := s.items[setKey(value)]
	return ok
}

func (s *SetValue) Values() []any {
	if s == nil {
		return []any{}
	}
	keys := make([]string, 0, len(s.items))
	for key := range s.items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		values = append(values, cloneValue(s.items[key]))
	}
	return values
}

func (s *SetValue) Union(other *SetValue) *SetValue {
	result := s.Clone()
	for key, value := range other.items {
		result.items[key] = cloneValue(value)
	}
	return result
}

func (s *SetValue) Intersection(other *SetValue) *SetValue {
	result := NewSetValue(nil)
	for key, value := range s.items {
		if _, ok := other.items[key]; ok {
			result.items[key] = cloneValue(value)
		}
	}
	return result
}

func (s *SetValue) Difference(other *SetValue) *SetValue {
	result := NewSetValue(nil)
	for key, value := range s.items {
		if _, ok := other.items[key]; ok {
			continue
		}
		result.items[key] = cloneValue(value)
	}
	return result
}

func (s *SetValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Values())
}

func asSet(value any) (*SetValue, bool) {
	switch typed := value.(type) {
	case *SetValue:
		return typed, true
	default:
		return nil, false
	}
}

func setKey(value any) string {
	encoded, err := json.Marshal(normalizeJSONValue(cloneValue(value)))
	if err != nil {
		return stringify(value)
	}
	return string(encoded)
}
