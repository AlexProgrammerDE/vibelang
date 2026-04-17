package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"

	"vibelang/internal/ast"
)

func registerTextBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("base64_encode", builtinBase64Encode, "string", "Encode text as standard base64.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("base64_decode", builtinBase64Decode, "string", "Decode standard base64 text back into UTF-8 text.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("url_encode", builtinURLEncode, "string", "Percent-encode text for safe use in URL query values.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("url_decode", builtinURLDecode, "string", "Decode percent-encoded URL query text.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("query_encode", builtinQueryEncode, "string", "Encode a dict of query parameters into a stable URL query string.", ast.Param{Name: "query", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, promptToolBuiltin("query_decode", builtinQueryDecode, "dict", "Decode a URL query string into a dict of strings and lists.", ast.Param{Name: "query", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("url_parse", builtinURLParse, "dict", "Parse a URL and return its components, including a decoded query dict.", ast.Param{Name: "raw_url", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("url_build", builtinURLBuild, "string", "Build a URL string from a dict with scheme, host, path, query, and fragment fields.", ast.Param{Name: "parts", Type: ast.TypeRef{Expr: "dict"}}))
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

func builtinQueryEncode(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("query_encode", args, 1); err != nil {
		return nil, err
	}
	query, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("query_encode expects query to be a dict")
	}
	return encodeQuery(query), nil
}

func builtinQueryDecode(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("query_decode", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("query_decode", args[0], "query")
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(text)
	if err != nil {
		return nil, err
	}
	return queryValuesToMap(values), nil
}

func builtinURLParse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("url_parse", args, 1); err != nil {
		return nil, err
	}
	rawURL, err := requireString("url_parse", args[0], "raw_url")
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"scheme":    parsed.Scheme,
		"host":      parsed.Host,
		"hostname":  parsed.Hostname(),
		"port":      parsed.Port(),
		"path":      parsed.Path,
		"raw_query": parsed.RawQuery,
		"query":     queryValuesToMap(parsed.Query()),
		"fragment":  parsed.Fragment,
	}
	if parsed.User != nil {
		result["username"] = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			result["password"] = password
		}
	}
	return result, nil
}

func builtinURLBuild(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("url_build", args, 1); err != nil {
		return nil, err
	}
	parts, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("url_build expects parts to be a dict")
	}

	built := &url.URL{
		Scheme:   mapStringValue(parts, "scheme"),
		Host:     mapStringValue(parts, "host"),
		Path:     mapStringValue(parts, "path"),
		Fragment: mapStringValue(parts, "fragment"),
	}

	username := mapStringValue(parts, "username")
	password := mapStringValue(parts, "password")
	switch {
	case username != "" && password != "":
		built.User = url.UserPassword(username, password)
	case username != "":
		built.User = url.User(username)
	}

	if rawQuery, ok := parts["raw_query"]; ok {
		text, err := requireString("url_build", rawQuery, "raw_query")
		if err != nil {
			return nil, err
		}
		built.RawQuery = text
	} else if query, ok := parts["query"]; ok {
		switch value := query.(type) {
		case string:
			built.RawQuery = value
		case map[string]any:
			built.RawQuery = encodeQuery(value)
		default:
			return nil, fmt.Errorf("url_build expects query to be a dict or string")
		}
	}

	return built.String(), nil
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

func encodeQuery(query map[string]any) string {
	if len(query) == 0 {
		return ""
	}

	values := make(url.Values, len(query))
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := query[key]
		if value == nil {
			continue
		}
		if list, ok := asList(value); ok {
			for _, item := range list {
				values.Add(key, stringify(item))
			}
			continue
		}
		values.Add(key, stringify(value))
	}
	return values.Encode()
}

func queryValuesToMap(values url.Values) map[string]any {
	result := make(map[string]any, len(values))
	for key, items := range values {
		switch len(items) {
		case 0:
			result[key] = ""
		case 1:
			result[key] = items[0]
		default:
			mapped := make([]any, 0, len(items))
			for _, item := range items {
				mapped = append(mapped, item)
			}
			result[key] = mapped
		}
	}
	return result
}

func mapStringValue(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	return stringify(raw)
}
