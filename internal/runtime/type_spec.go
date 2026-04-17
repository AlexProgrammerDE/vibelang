package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type typeSpec struct {
	kind   string
	name   string
	args   []*typeSpec
	fields []typeField
}

type typeField struct {
	name string
	spec *typeSpec
}

type typeParser struct {
	text string
	pos  int
}

var typeSpecCache sync.Map

func parseTypeSpec(typeExpr string) (*typeSpec, error) {
	trimmed := strings.TrimSpace(typeExpr)
	if trimmed == "" {
		return &typeSpec{kind: "named", name: "any"}, nil
	}
	if cached, ok := typeSpecCache.Load(trimmed); ok {
		return cached.(*typeSpec), nil
	}

	parser := &typeParser{text: trimmed}
	spec, err := parser.parseType()
	if err != nil {
		return nil, err
	}
	parser.skipSpaces()
	if !parser.done() {
		return nil, fmt.Errorf("invalid type expression %q near %q", typeExpr, parser.text[parser.pos:])
	}

	typeSpecCache.Store(trimmed, spec)
	return spec, nil
}

func (p *typeParser) parseType() (*typeSpec, error) {
	p.skipSpaces()
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	spec := &typeSpec{
		kind: "named",
		name: strings.ToLower(name),
	}

	p.skipSpaces()
	switch {
	case p.consume('['):
		args, err := p.parseTypeList(']')
		if err != nil {
			return nil, err
		}
		spec.kind = strings.ToLower(name)
		spec.args = args
	case p.consume('{'):
		if spec.name != "dict" && spec.name != "record" {
			return nil, fmt.Errorf("only dict{...} or record{...} can declare typed fields")
		}
		fields, err := p.parseFieldList()
		if err != nil {
			return nil, err
		}
		spec.kind = "record"
		spec.fields = fields
	}

	return spec, nil
}

func (p *typeParser) parseTypeList(closing byte) ([]*typeSpec, error) {
	args := make([]*typeSpec, 0)
	p.skipSpaces()
	if p.consume(closing) {
		return args, nil
	}

	for {
		spec, err := p.parseType()
		if err != nil {
			return nil, err
		}
		args = append(args, spec)

		p.skipSpaces()
		if p.consume(',') {
			continue
		}
		if p.consume(closing) {
			break
		}
		return nil, fmt.Errorf("expected %q in type expression", string(closing))
	}

	return args, nil
}

func (p *typeParser) parseFieldList() ([]typeField, error) {
	fields := make([]typeField, 0)
	p.skipSpaces()
	if p.consume('}') {
		return fields, nil
	}

	for {
		name, err := p.parseFieldName()
		if err != nil {
			return nil, err
		}
		p.skipSpaces()
		if !p.consume(':') {
			return nil, fmt.Errorf("expected ':' after field name %q", name)
		}
		spec, err := p.parseType()
		if err != nil {
			return nil, err
		}
		fields = append(fields, typeField{name: name, spec: spec})

		p.skipSpaces()
		if p.consume(',') {
			continue
		}
		if p.consume('}') {
			break
		}
		return nil, fmt.Errorf("expected ',' or '}' in record type")
	}

	return fields, nil
}

func (p *typeParser) parseFieldName() (string, error) {
	p.skipSpaces()
	if p.done() {
		return "", fmt.Errorf("expected field name")
	}
	switch p.peek() {
	case '"', '\'':
		return p.parseQuotedString()
	default:
		return p.parseIdentifier()
	}
}

func (p *typeParser) parseQuotedString() (string, error) {
	quote := p.peek()
	start := p.pos
	p.pos++
	escaped := false
	for !p.done() {
		ch := p.peek()
		p.pos++
		if escaped {
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case quote:
			value, err := strconv.Unquote(p.text[start:p.pos])
			if err != nil {
				return "", err
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("unterminated quoted field name")
}

func (p *typeParser) parseIdentifier() (string, error) {
	p.skipSpaces()
	if p.done() {
		return "", fmt.Errorf("expected type identifier")
	}
	start := p.pos
	for !p.done() {
		ch := p.peek()
		if isTypeIdentifierPart(ch) {
			p.pos++
			continue
		}
		break
	}
	if start == p.pos {
		return "", fmt.Errorf("expected type identifier")
	}
	return p.text[start:p.pos], nil
}

func (p *typeParser) skipSpaces() {
	for !p.done() {
		switch p.peek() {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *typeParser) consume(ch byte) bool {
	p.skipSpaces()
	if p.done() || p.peek() != ch {
		return false
	}
	p.pos++
	return true
}

func (p *typeParser) peek() byte {
	return p.text[p.pos]
}

func (p *typeParser) done() bool {
	return p.pos >= len(p.text)
}

func isTypeIdentifierPart(ch byte) bool {
	return ch == '_' || ch == '.' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func (s *typeSpec) coerceValue(value any) (any, error) {
	switch s.kind {
	case "named":
		return coerceNamedType(s.name, value)
	case "list":
		list, ok := asList(value)
		if !ok {
			return nil, fmt.Errorf("expected list, got %s", typeName(value))
		}
		itemSpec := anyTypeSpec()
		if len(s.args) > 0 {
			itemSpec = s.args[0]
		}
		result := make([]any, 0, len(list))
		for index, item := range list {
			coerced, err := itemSpec.coerceValue(item)
			if err != nil {
				return nil, fmt.Errorf("list item %d: %w", index, err)
			}
			result = append(result, coerced)
		}
		return result, nil
	case "set":
		set, ok := asSet(value)
		if !ok {
			list, listOK := asList(value)
			if !listOK {
				return nil, fmt.Errorf("expected set, got %s", typeName(value))
			}
			set = NewSetValue(list)
		}
		itemSpec := anyTypeSpec()
		if len(s.args) > 0 {
			itemSpec = s.args[0]
		}
		items := set.Values()
		coerced := make([]any, 0, len(items))
		for index, item := range items {
			value, err := itemSpec.coerceValue(item)
			if err != nil {
				return nil, fmt.Errorf("set item %d: %w", index, err)
			}
			coerced = append(coerced, value)
		}
		return NewSetValue(coerced), nil
	case "dict":
		dict, ok := asMap(value)
		if !ok {
			return nil, fmt.Errorf("expected dict, got %s", typeName(value))
		}
		valueSpec := anyTypeSpec()
		switch len(s.args) {
		case 1:
			valueSpec = s.args[0]
		case 2:
			valueSpec = s.args[1]
		}
		result := make(map[string]any, len(dict))
		for key, item := range dict {
			coerced, err := valueSpec.coerceValue(item)
			if err != nil {
				return nil, fmt.Errorf("dict field %q: %w", key, err)
			}
			result[key] = coerced
		}
		return result, nil
	case "record":
		dict, ok := asMap(value)
		if !ok {
			return nil, fmt.Errorf("expected dict, got %s", typeName(value))
		}
		result := make(map[string]any, len(s.fields))
		allowed := make(map[string]struct{}, len(s.fields))
		for _, field := range s.fields {
			allowed[field.name] = struct{}{}
			item, exists := dict[field.name]
			if !exists {
				if field.spec.allowsNone() {
					result[field.name] = nil
					continue
				}
				return nil, fmt.Errorf("missing required field %q", field.name)
			}
			coerced, err := field.spec.coerceValue(item)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", field.name, err)
			}
			result[field.name] = coerced
		}
		for key := range dict {
			if _, ok := allowed[key]; ok {
				continue
			}
			return nil, fmt.Errorf("unexpected field %q", key)
		}
		return result, nil
	case "optional":
		if value == nil {
			return nil, nil
		}
		if len(s.args) == 0 {
			return value, nil
		}
		return s.args[0].coerceValue(value)
	case "oneof":
		if len(s.args) == 0 {
			return value, nil
		}
		errs := make([]string, 0, len(s.args))
		for _, option := range s.args {
			coerced, err := option.coerceValue(value)
			if err == nil {
				return coerced, nil
			}
			errs = append(errs, err.Error())
		}
		return nil, fmt.Errorf("did not match any allowed type: %s", strings.Join(errs, "; "))
	case "tuple":
		list, ok := asList(value)
		if !ok {
			return nil, fmt.Errorf("expected tuple, got %s", typeName(value))
		}
		if len(list) != len(s.args) {
			return nil, fmt.Errorf("expected tuple of length %d, got %d", len(s.args), len(list))
		}
		result := make([]any, 0, len(list))
		for index, item := range list {
			coerced, err := s.args[index].coerceValue(item)
			if err != nil {
				return nil, fmt.Errorf("tuple item %d: %w", index, err)
			}
			result = append(result, coerced)
		}
		return result, nil
	default:
		return value, nil
	}
}

func (s *typeSpec) allowsNone() bool {
	switch s.kind {
	case "named":
		return s.name == "none"
	case "optional":
		return true
	case "oneof":
		for _, option := range s.args {
			if option.allowsNone() {
				return true
			}
		}
	}
	return false
}

func (s *typeSpec) jsonSchema() map[string]any {
	switch s.kind {
	case "named":
		switch s.name {
		case "any":
			return map[string]any{}
		case "string":
			return map[string]any{"type": "string"}
		case "int":
			return map[string]any{"type": "integer"}
		case "float":
			return map[string]any{"type": "number"}
		case "bool":
			return map[string]any{"type": "boolean"}
		case "none":
			return map[string]any{"type": "null"}
		case "list":
			return map[string]any{
				"type":  "array",
				"items": map[string]any{},
			}
		case "set":
			return map[string]any{
				"type":        "array",
				"items":       map[string]any{},
				"uniqueItems": true,
			}
		case "dict":
			return map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			}
		default:
			return map[string]any{}
		}
	case "list":
		return map[string]any{
			"type":  "array",
			"items": schemaForArg(s.args, 0),
		}
	case "set":
		return map[string]any{
			"type":        "array",
			"items":       schemaForArg(s.args, 0),
			"uniqueItems": true,
		}
	case "dict":
		schema := map[string]any{
			"type": "object",
		}
		valueSchema := map[string]any{}
		switch len(s.args) {
		case 1:
			valueSchema = s.args[0].jsonSchema()
		case 2:
			valueSchema = s.args[1].jsonSchema()
		}
		if len(valueSchema) == 0 {
			schema["additionalProperties"] = true
		} else {
			schema["additionalProperties"] = valueSchema
		}
		return schema
	case "record":
		properties := make(map[string]any, len(s.fields))
		required := make([]string, 0, len(s.fields))
		for _, field := range s.fields {
			properties[field.name] = field.spec.jsonSchema()
			if !field.spec.allowsNone() {
				required = append(required, field.name)
			}
		}
		schema := map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	case "optional":
		items := []any{
			map[string]any{"type": "null"},
		}
		if len(s.args) > 0 {
			items = append(items, s.args[0].jsonSchema())
		} else {
			items = append(items, map[string]any{})
		}
		return map[string]any{"anyOf": items}
	case "oneof":
		items := make([]any, 0, len(s.args))
		for _, option := range s.args {
			items = append(items, option.jsonSchema())
		}
		if len(items) == 0 {
			return map[string]any{}
		}
		return map[string]any{"anyOf": items}
	case "tuple":
		prefix := make([]any, 0, len(s.args))
		for _, item := range s.args {
			prefix = append(prefix, item.jsonSchema())
		}
		return map[string]any{
			"type":        "array",
			"prefixItems": prefix,
			"minItems":    len(s.args),
			"maxItems":    len(s.args),
		}
	default:
		return map[string]any{}
	}
}

func coerceNamedType(name string, value any) (any, error) {
	switch name {
	case "any":
		return value, nil
	case "string":
		return stringify(value), nil
	case "int":
		return coerceInt(value)
	case "float":
		return coerceFloat(value)
	case "bool":
		return coerceBool(value)
	case "none":
		if value != nil {
			return nil, fmt.Errorf("expected none, got %s", typeName(value))
		}
		return nil, nil
	case "list":
		if list, ok := asList(value); ok {
			return list, nil
		}
		return nil, fmt.Errorf("expected list, got %s", typeName(value))
	case "set":
		if set, ok := asSet(value); ok {
			return set, nil
		}
		if list, ok := asList(value); ok {
			return NewSetValue(list), nil
		}
		return nil, fmt.Errorf("expected set, got %s", typeName(value))
	case "dict":
		if dict, ok := asMap(value); ok {
			return dict, nil
		}
		return nil, fmt.Errorf("expected dict, got %s", typeName(value))
	default:
		return value, nil
	}
}

func anyTypeSpec() *typeSpec {
	return &typeSpec{kind: "named", name: "any"}
}

func schemaForArg(args []*typeSpec, index int) map[string]any {
	if len(args) <= index || args[index] == nil {
		return map[string]any{}
	}
	return args[index].jsonSchema()
}
