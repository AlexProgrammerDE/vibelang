package runtime

import "fmt"

type Environment struct {
	parent *Environment
	values map[string]any
}

func NewEnvironment(parent *Environment) *Environment {
	return &Environment{
		parent: parent,
		values: make(map[string]any),
	}
}

func (e *Environment) Define(name string, value any) {
	e.values[name] = value
}

func (e *Environment) Set(name string, value any) {
	e.values[name] = value
}

func (e *Environment) Assign(name string, value any) bool {
	if _, ok := e.values[name]; ok {
		e.values[name] = value
		return true
	}
	if e.parent != nil {
		return e.parent.Assign(name, value)
	}
	return false
}

func (e *Environment) Get(name string) (any, error) {
	if value, ok := e.values[name]; ok {
		return value, nil
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return nil, fmt.Errorf("undefined name %q", name)
}

func (e *Environment) SnapshotValues() map[string]any {
	values := make(map[string]any)
	e.collectVisibleValues(values)
	return values
}

func (e *Environment) ExportedValues() map[string]any {
	values := make(map[string]any, len(e.values))
	for name, value := range e.values {
		if name == "__file__" || name == "__dir__" || len(name) > 0 && name[0] == '_' {
			continue
		}
		values[name] = value
	}
	return values
}

func (e *Environment) collectVisibleValues(values map[string]any) {
	if e == nil {
		return
	}
	if e.parent != nil {
		e.parent.collectVisibleValues(values)
	}
	for name, value := range e.values {
		if _, ok := value.(Callable); ok {
			continue
		}
		values[name] = value
	}
}
