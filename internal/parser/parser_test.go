package parser

import (
	"testing"

	"vibelang/internal/ast"
)

func TestParseFunctionPreservesRawBody(t *testing.T) {
	source := `def summarize(name: string) -> string:
    Write one short line about ${name}.

    Mention the city if it is relevant.
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(program.Statements))
	}

	function, ok := program.Statements[0].(*ast.FunctionDef)
	if !ok {
		t.Fatalf("expected FunctionDef, got %T", program.Statements[0])
	}

	want := "Write one short line about ${name}.\n\nMention the city if it is relevant."
	if function.Body != want {
		t.Fatalf("unexpected function body\nwant: %q\ngot:  %q", want, function.Body)
	}

	if function.ReturnType.String() != "string" {
		t.Fatalf("expected return type string, got %q", function.ReturnType.String())
	}
}

func TestParseNestedBlocks(t *testing.T) {
	source := `total = 0
for value in range(1, 4):
    if value > 1:
        total = total + value
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}

	loop, ok := program.Statements[1].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", program.Statements[1])
	}
	if len(loop.Body) != 1 {
		t.Fatalf("expected for-body length 1, got %d", len(loop.Body))
	}

	ifStmt, ok := loop.Body[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt inside loop, got %T", loop.Body[0])
	}
	if len(ifStmt.Then) != 1 {
		t.Fatalf("expected if-body length 1, got %d", len(ifStmt.Then))
	}
}

func TestParseMatchStatement(t *testing.T) {
	source := `match packet:
    case {"type": "ping"}:
        print("pong")
    case {"type": "message", "payload": [head, tail]}:
        print(head)
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(program.Statements))
	}

	matchStmt, ok := program.Statements[0].(*ast.MatchStmt)
	if !ok {
		t.Fatalf("expected MatchStmt, got %T", program.Statements[0])
	}

	if len(matchStmt.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(matchStmt.Cases))
	}

	dictPattern, ok := matchStmt.Cases[1].Pattern.(*ast.DictLiteral)
	if !ok {
		t.Fatalf("expected dict pattern, got %T", matchStmt.Cases[1].Pattern)
	}
	if len(dictPattern.Items) != 2 {
		t.Fatalf("expected 2 dict pattern items, got %d", len(dictPattern.Items))
	}
}

func TestParseTryExceptFinally(t *testing.T) {
	source := `try:
    fail("boom")
except err:
    print(err)
finally:
    print("done")
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(program.Statements))
	}

	tryStmt, ok := program.Statements[0].(*ast.TryStmt)
	if !ok {
		t.Fatalf("expected TryStmt, got %T", program.Statements[0])
	}
	if tryStmt.ErrorName != "err" {
		t.Fatalf("expected except binding err, got %q", tryStmt.ErrorName)
	}
	if len(tryStmt.Body) != 1 || len(tryStmt.Except) != 1 || len(tryStmt.Finally) != 1 {
		t.Fatalf("unexpected try body sizes: %#v", tryStmt)
	}
}

func TestParseInlinePromptsPreserveRawText(t *testing.T) {
	source := `path = "notes.txt"
digits = * find the first 5 digits of pi, then return them as a string.
if * check whether ${path} already exists:
    * delete the file at ${path}, then confirm success.
else:
    * create the file at ${path} with ${digits}.
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(program.Statements))
	}

	assign, ok := program.Statements[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[1])
	}

	valuePrompt, ok := assign.Value.(*ast.PromptExpr)
	if !ok {
		t.Fatalf("expected PromptExpr on assignment RHS, got %T", assign.Value)
	}

	wantValuePrompt := "find the first 5 digits of pi, then return them as a string."
	if valuePrompt.Text != wantValuePrompt {
		t.Fatalf("unexpected assignment prompt\nwant: %q\ngot:  %q", wantValuePrompt, valuePrompt.Text)
	}

	ifStmt, ok := program.Statements[2].(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", program.Statements[2])
	}

	conditionPrompt, ok := ifStmt.Condition.(*ast.PromptExpr)
	if !ok {
		t.Fatalf("expected PromptExpr in if condition, got %T", ifStmt.Condition)
	}

	wantConditionPrompt := "check whether ${path} already exists"
	if conditionPrompt.Text != wantConditionPrompt {
		t.Fatalf("unexpected condition prompt\nwant: %q\ngot:  %q", wantConditionPrompt, conditionPrompt.Text)
	}

	thenStmt, ok := ifStmt.Then[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt in then branch, got %T", ifStmt.Then[0])
	}
	thenPrompt, ok := thenStmt.Expr.(*ast.PromptExpr)
	if !ok {
		t.Fatalf("expected PromptExpr in then branch, got %T", thenStmt.Expr)
	}

	wantThenPrompt := "delete the file at ${path}, then confirm success."
	if thenPrompt.Text != wantThenPrompt {
		t.Fatalf("unexpected then prompt\nwant: %q\ngot:  %q", wantThenPrompt, thenPrompt.Text)
	}
}

func TestParseListLiteralWithMultipleStrings(t *testing.T) {
	source := `values = ["alpha", "beta", "gamma"]
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(program.Statements))
	}

	assign, ok := program.Statements[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[0])
	}

	list, ok := assign.Value.(*ast.ListLiteral)
	if !ok {
		t.Fatalf("expected ListLiteral, got %T", assign.Value)
	}

	if len(list.Elements) != 3 {
		t.Fatalf("expected 3 list elements, got %d", len(list.Elements))
	}
}

func TestParseFunctionWithDefaultParametersAndKeywordCall(t *testing.T) {
	source := `def summarize(name: string, tone: string = "dry") -> string:
    Speak in a ${tone} tone about ${name}.

result = summarize(name="Ada", tone="playful")
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}

	function, ok := program.Statements[0].(*ast.FunctionDef)
	if !ok {
		t.Fatalf("expected FunctionDef, got %T", program.Statements[0])
	}

	if len(function.Params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(function.Params))
	}

	if function.Params[1].Name != "tone" {
		t.Fatalf("expected second parameter to be tone, got %q", function.Params[1].Name)
	}

	callAssign, ok := program.Statements[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[1])
	}

	call, ok := callAssign.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", callAssign.Value)
	}

	if len(call.Arguments) != 2 {
		t.Fatalf("expected 2 call arguments, got %d", len(call.Arguments))
	}
}

func TestParseModuleImports(t *testing.T) {
	source := `import "./shared.vibe" as shared
from "./shared.vibe" import format_name, helper as alias_helper
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}
}

func TestParseMemberAccess(t *testing.T) {
	source := `import "./shared.vibe" as shared
result = shared.format_name("Ada")
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}

	assign, ok := program.Statements[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[1])
	}

	call, ok := assign.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", assign.Value)
	}

	member, ok := call.Callee.(*ast.MemberExpr)
	if !ok {
		t.Fatalf("expected MemberExpr, got %T", call.Callee)
	}

	if member.Name != "format_name" {
		t.Fatalf("expected member name format_name, got %q", member.Name)
	}
}

func TestParseSliceExpressions(t *testing.T) {
	source := `items = ["alpha", "beta", "gamma", "delta"]
middle = items[1:3]
tail = items[2:]
copy = items[:]
reverse = items[::-1]
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 5 {
		t.Fatalf("expected 5 statements, got %d", len(program.Statements))
	}
}

func TestParseComprehensions(t *testing.T) {
	source := `names = [upper(name) for name in ["ada", "grace", "linus"] if "a" in name]
lengths = {name: len(name) for name in names if len(name) > 3}
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}

	listAssign, ok := program.Statements[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[0])
	}
	if _, ok := listAssign.Value.(*ast.ListComprehensionExpr); !ok {
		t.Fatalf("expected ListComprehensionExpr, got %T", listAssign.Value)
	}

	dictAssign, ok := program.Statements[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[1])
	}
	if _, ok := dictAssign.Value.(*ast.DictComprehensionExpr); !ok {
		t.Fatalf("expected DictComprehensionExpr, got %T", dictAssign.Value)
	}
}

func TestParseMacrosAndMacroCalls(t *testing.T) {
	source := `macro double_expr(value: int) -> int:
    Return the vibelang expression that doubles ${value}.

result = @double_expr(21)
`

	program, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}

	macro, ok := program.Statements[0].(*ast.MacroDef)
	if !ok {
		t.Fatalf("expected MacroDef, got %T", program.Statements[0])
	}

	if macro.Name != "double_expr" {
		t.Fatalf("expected macro name double_expr, got %q", macro.Name)
	}

	assign, ok := program.Statements[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected AssignStmt, got %T", program.Statements[1])
	}

	call, ok := assign.Value.(*ast.MacroCallExpr)
	if !ok {
		t.Fatalf("expected MacroCallExpr, got %T", assign.Value)
	}

	if len(call.Arguments) != 1 {
		t.Fatalf("expected 1 macro argument, got %d", len(call.Arguments))
	}
}
