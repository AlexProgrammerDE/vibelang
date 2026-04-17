package runtime

import (
	"testing"

	"vibelang/internal/ast"
)

func TestBuildAIActionSchemaConstrainsToolArguments(t *testing.T) {
	schema, err := buildAIActionSchema("string", []ToolSpec{
		{
			Name: "fetch_weather",
			Params: []ast.Param{
				{Name: "city", Type: ast.TypeRef{Expr: "string"}},
				{Name: "days", Type: ast.TypeRef{Expr: "int"}, DefaultText: "3"},
			},
			ReturnType: ast.TypeRef{Expr: "dict{summary: string}"},
		},
	})
	if err != nil {
		t.Fatalf("buildAIActionSchema returned error: %v", err)
	}

	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 2 {
		t.Fatalf("expected return and call variants, got %#v", schema["oneOf"])
	}

	callVariant, ok := variants[1].(map[string]any)
	if !ok {
		t.Fatalf("expected call variant to be an object, got %#v", variants[1])
	}
	properties, ok := callVariant["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected call variant properties, got %#v", callVariant["properties"])
	}
	callSchema, ok := properties["call"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested call schema, got %#v", properties["call"])
	}
	callProperties, ok := callSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested call properties, got %#v", callSchema["properties"])
	}
	arguments, ok := callProperties["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("expected arguments schema object, got %#v", callProperties["arguments"])
	}
	if arguments["additionalProperties"] != false {
		t.Fatalf("expected arguments schema to reject unknown keys, got %#v", arguments["additionalProperties"])
	}

	argumentProperties, ok := arguments["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected arguments properties, got %#v", arguments["properties"])
	}

	citySchema, ok := argumentProperties["city"].(map[string]any)
	if !ok || citySchema["type"] != "string" {
		t.Fatalf("expected city argument to be typed as string, got %#v", argumentProperties["city"])
	}
	daysSchema, ok := argumentProperties["days"].(map[string]any)
	if !ok || daysSchema["type"] != "integer" {
		t.Fatalf("expected days argument to be typed as integer, got %#v", argumentProperties["days"])
	}

	required, ok := arguments["required"].([]string)
	if !ok {
		t.Fatalf("expected required argument list, got %#v", arguments["required"])
	}
	if len(required) != 1 || required[0] != "city" {
		t.Fatalf("expected only city to be required, got %#v", required)
	}
}

func TestBuildMacroActionSchemaConstrainsToolArguments(t *testing.T) {
	schema, err := buildMacroActionSchema([]ToolSpec{
		{
			Name: "range",
			Params: []ast.Param{
				{Name: "start", Type: ast.TypeRef{Expr: "int"}, DefaultText: "0"},
				{Name: "stop", Type: ast.TypeRef{Expr: "int"}},
				{Name: "step", Type: ast.TypeRef{Expr: "int"}, DefaultText: "1"},
			},
			ReturnType: ast.TypeRef{Expr: "list[int]"},
		},
	})
	if err != nil {
		t.Fatalf("buildMacroActionSchema returned error: %v", err)
	}

	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 2 {
		t.Fatalf("expected expand and call variants, got %#v", schema["oneOf"])
	}

	callVariant, ok := variants[1].(map[string]any)
	if !ok {
		t.Fatalf("expected call variant to be an object, got %#v", variants[1])
	}
	properties, ok := callVariant["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected call variant properties, got %#v", callVariant["properties"])
	}
	callSchema, ok := properties["call"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested call schema, got %#v", properties["call"])
	}
	callProperties, ok := callSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested call properties, got %#v", callSchema["properties"])
	}
	arguments, ok := callProperties["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("expected arguments schema object, got %#v", callProperties["arguments"])
	}

	required, ok := arguments["required"].([]string)
	if !ok {
		t.Fatalf("expected required argument list, got %#v", arguments["required"])
	}
	if len(required) != 1 || required[0] != "stop" {
		t.Fatalf("expected only stop to be required, got %#v", required)
	}
}
