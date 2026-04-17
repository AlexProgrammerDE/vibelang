package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"

	"vibelang/internal/ast"
)

func registerTextBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("base64_encode", builtinBase64Encode, "string", "Encode text as standard base64.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("base64_decode", builtinBase64Decode, "string", "Decode standard base64 text back into UTF-8 text.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("url_encode", builtinURLEncode, "string", "Percent-encode text for safe use in URL query values.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("url_decode", builtinURLDecode, "string", "Decode percent-encoded URL query text.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("sha256", builtinSHA256, "string", "Return the lowercase hexadecimal SHA-256 digest for a string.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("regex_match", builtinRegexMatch, "bool", "Return true when the regular expression matches anywhere in the text.", ast.Param{Name: "pattern", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("regex_find_all", builtinRegexFindAll, "list[string]", "Return all regex matches in the text, in order.", ast.Param{Name: "pattern", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("regex_replace", builtinRegexReplace, "string", "Replace every regex match in the text with the replacement string.", ast.Param{Name: "pattern", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "replacement", Type: ast.TypeRef{Expr: "string"}}))
}

func builtinBase64Encode(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("base64_encode", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("base64_encode", args[0], "text")
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.EncodeToString([]byte(text)), nil
}

func builtinBase64Decode(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("base64_decode", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("base64_decode", args[0], "text")
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, err
	}
	return string(decoded), nil
}

func builtinURLEncode(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("url_encode", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("url_encode", args[0], "text")
	if err != nil {
		return nil, err
	}
	return url.QueryEscape(text), nil
}

func builtinURLDecode(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("url_decode", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("url_decode", args[0], "text")
	if err != nil {
		return nil, err
	}
	return url.QueryUnescape(text)
}

func builtinSHA256(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("sha256", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("sha256", args[0], "text")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:]), nil
}

func builtinRegexMatch(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("regex_match", args, 2); err != nil {
		return nil, err
	}
	pattern, text, err := regexArgs("regex_match", args)
	if err != nil {
		return nil, err
	}
	return regexp.MatchString(pattern, text)
}

func builtinRegexFindAll(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("regex_find_all", args, 2); err != nil {
		return nil, err
	}
	pattern, text, err := regexArgs("regex_find_all", args)
	if err != nil {
		return nil, err
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	matches := compiled.FindAllString(text, -1)
	result := make([]any, 0, len(matches))
	for _, match := range matches {
		result = append(result, match)
	}
	return result, nil
}

func builtinRegexReplace(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("regex_replace", args, 3); err != nil {
		return nil, err
	}
	pattern, text, err := regexArgs("regex_replace", args[:2])
	if err != nil {
		return nil, err
	}
	replacement, err := requireString("regex_replace", args[2], "replacement")
	if err != nil {
		return nil, err
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return compiled.ReplaceAllString(text, replacement), nil
}

func regexArgs(name string, args []any) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s expects pattern and text", name)
	}
	pattern, err := requireString(name, args[0], "pattern")
	if err != nil {
		return "", "", err
	}
	text, err := requireString(name, args[1], "text")
	if err != nil {
		return "", "", err
	}
	return pattern, text, nil
}
