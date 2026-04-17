package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"vibelang/internal/ast"
)

type httpRoute struct {
	pattern string
	methods map[string]struct{}
	handler Callable
}

func registerHTTPServerBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("route_match", builtinRouteMatch, "dict{matched: bool, params: dict[string, string]}", "Match a request path against a route pattern like /users/:id or /assets/*path and return matched plus any extracted params.", ast.Param{Name: "pattern", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "request_path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "http_serve",
		call: builtinHTTPServe,
		tool: &ToolSpec{
			Name:       "http_serve",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Start an HTTP server and return a dict with handle and address.",
			Params: []ast.Param{
				{Name: "address", Type: ast.TypeRef{Expr: "string"}},
				{Name: "handler"},
				{Name: "read_timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "15000"},
				{Name: "write_timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "15000"},
			},
		},
		defaults: map[string]any{
			"read_timeout_ms":  int64(15000),
			"write_timeout_ms": int64(15000),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "http_serve_routes",
		call: builtinHTTPServeRoutes,
		tool: &ToolSpec{
			Name:       "http_serve_routes",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Start an HTTP server backed by ordered route definitions. Each route dict must include pattern and handler, and may include methods.",
			Params: []ast.Param{
				{Name: "address", Type: ast.TypeRef{Expr: "string"}},
				{Name: "routes", Type: ast.TypeRef{Expr: "list"}},
				{Name: "fallback", DefaultText: "none"},
				{Name: "read_timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "15000"},
				{Name: "write_timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "15000"},
			},
		},
		defaults: map[string]any{
			"fallback":         nil,
			"read_timeout_ms":  int64(15000),
			"write_timeout_ms": int64(15000),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "http_server_stop",
		call: builtinHTTPServerStop,
		tool: &ToolSpec{
			Name:       "http_server_stop",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "Gracefully stop an HTTP server by handle.",
			Params: []ast.Param{
				{Name: "handle", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "5000"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(5000),
		},
		bindArgs: true,
	})
}

func builtinRouteMatch(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("route_match", args, 2); err != nil {
		return nil, err
	}
	pattern, err := requireString("route_match", args[0], "pattern")
	if err != nil {
		return nil, err
	}
	requestPath, err := requireString("route_match", args[1], "request_path")
	if err != nil {
		return nil, err
	}

	matched, params := routeMatch(pattern, requestPath)
	return map[string]any{
		"matched": matched,
		"params":  params,
	}, nil
}

func builtinHTTPServe(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("http_serve", args, 4); err != nil {
		return nil, err
	}
	address, err := requireString("http_serve", args[0], "address")
	if err != nil {
		return nil, err
	}
	handler, ok := args[1].(Callable)
	if !ok {
		return nil, fmt.Errorf("http_serve expects handler to be a function")
	}
	readTimeoutMS, err := requireInt("http_serve", args[2], "read_timeout_ms")
	if err != nil {
		return nil, err
	}
	writeTimeoutMS, err := requireInt("http_serve", args[3], "write_timeout_ms")
	if err != nil {
		return nil, err
	}

	return startHTTPServer(interpreter, address, readTimeoutMS, writeTimeoutMS, func(writer http.ResponseWriter, request *http.Request) {
		interpreter.serveAIHTTP(writer, request, handler)
	})
}

func builtinHTTPServeRoutes(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("http_serve_routes", args, 5); err != nil {
		return nil, err
	}
	address, err := requireString("http_serve_routes", args[0], "address")
	if err != nil {
		return nil, err
	}
	routes, err := parseHTTPRoutes(args[1])
	if err != nil {
		return nil, fmt.Errorf("http_serve_routes routes: %w", err)
	}
	var fallback Callable
	if args[2] != nil {
		callable, ok := args[2].(Callable)
		if !ok {
			return nil, fmt.Errorf("http_serve_routes expects fallback to be a function when provided")
		}
		fallback = callable
	}
	readTimeoutMS, err := requireInt("http_serve_routes", args[3], "read_timeout_ms")
	if err != nil {
		return nil, err
	}
	writeTimeoutMS, err := requireInt("http_serve_routes", args[4], "write_timeout_ms")
	if err != nil {
		return nil, err
	}

	return startHTTPServer(interpreter, address, readTimeoutMS, writeTimeoutMS, func(writer http.ResponseWriter, request *http.Request) {
		interpreter.serveAIHTTPRoutes(writer, request, routes, fallback)
	})
}

func builtinHTTPServerStop(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("http_server_stop", args, 2); err != nil {
		return nil, err
	}
	handleID, err := requireString("http_server_stop", args[0], "handle")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("http_server_stop", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}
	handle, ok := interpreter.closeServer(handleID)
	if !ok {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	if err := handle.server.Shutdown(ctx); err != nil {
		return nil, err
	}
	interpreter.incrementMetric("http_servers_stopped_total", 1)
	return true, nil
}

func startHTTPServer(interpreter *Interpreter, address string, readTimeoutMS, writeTimeoutMS int64, handler http.HandlerFunc) (map[string]any, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	serverHandle := interpreter.nextHandle("http_server")
	server := &http.Server{
		Handler:      handler,
		ReadTimeout:  time.Duration(readTimeoutMS) * time.Millisecond,
		WriteTimeout: time.Duration(writeTimeoutMS) * time.Millisecond,
	}

	interpreter.storeServer(serverHandle, &httpServerHandle{
		server:   server,
		listener: listener,
		address:  listener.Addr().String(),
	})
	interpreter.incrementMetric("http_servers_started_total", 1)

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			interpreter.incrementMetric("http_server_errors_total", 1)
			interpreter.tracef("http server %s stopped with error: %v", serverHandle, err)
		}
	}()

	return map[string]any{
		"handle":  serverHandle,
		"address": listener.Addr().String(),
	}, nil
}

func (i *Interpreter) serveAIHTTP(writer http.ResponseWriter, request *http.Request, handler Callable) {
	i.incrementMetric("http_requests_total", 1)

	payload, err := buildHTTPRequestPayload(request)
	if err != nil {
		i.incrementMetric("http_request_errors_total", 1)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	result, err := handler.Call(request.Context(), i, []CallArgument{{Value: payload}})
	if err != nil {
		i.incrementMetric("http_request_errors_total", 1)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	status, headers, body, err := formatHTTPHandlerResponse(result)
	if err != nil {
		i.incrementMetric("http_request_errors_total", 1)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	i.writeHTTPResponse(writer, status, headers, body)
}

func (i *Interpreter) serveAIHTTPRoutes(writer http.ResponseWriter, request *http.Request, routes []httpRoute, fallback Callable) {
	i.incrementMetric("http_requests_total", 1)

	payload, err := buildHTTPRequestPayload(request)
	if err != nil {
		i.incrementMetric("http_request_errors_total", 1)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	allowed := make(map[string]struct{})
	for _, route := range routes {
		matched, params := routeMatch(route.pattern, request.URL.Path)
		if !matched {
			continue
		}
		if !route.allowsMethod(request.Method) {
			for method := range route.methods {
				allowed[method] = struct{}{}
			}
			continue
		}
		payloadWithRoute := mergeRoutePayload(payload, route.pattern, params, true)
		result, err := route.handler.Call(request.Context(), i, []CallArgument{{Value: payloadWithRoute}})
		if err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		status, headers, body, err := formatHTTPHandlerResponse(result)
		if err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		i.writeHTTPResponse(writer, status, headers, body)
		return
	}

	if fallback != nil {
		payloadWithRoute := mergeRoutePayload(payload, "", map[string]any{}, false)
		result, err := fallback.Call(request.Context(), i, []CallArgument{{Value: payloadWithRoute}})
		if err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		status, headers, body, err := formatHTTPHandlerResponse(result)
		if err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		i.writeHTTPResponse(writer, status, headers, body)
		return
	}

	if len(allowed) > 0 {
		headers := map[string]string{
			"Allow":        allowedMethodsHeader(allowed),
			"Content-Type": "text/plain; charset=utf-8",
		}
		i.writeHTTPResponse(writer, http.StatusMethodNotAllowed, headers, "method not allowed")
		return
	}

	i.writeHTTPResponse(writer, http.StatusNotFound, map[string]string{
		"Content-Type": "text/plain; charset=utf-8",
	}, "not found")
}

func (i *Interpreter) writeHTTPResponse(writer http.ResponseWriter, status int, headers map[string]string, body string) {
	for key, value := range headers {
		writer.Header().Set(key, value)
	}
	writer.WriteHeader(status)
	if _, err := io.WriteString(writer, body); err != nil {
		i.incrementMetric("http_request_errors_total", 1)
		return
	}
	i.incrementMetric("http_responses_total", 1)
}

func parseHTTPRoutes(value any) ([]httpRoute, error) {
	items, ok := asList(value)
	if !ok {
		return nil, fmt.Errorf("expected a list of route dictionaries")
	}
	routes := make([]httpRoute, 0, len(items))
	for index, item := range items {
		entry, ok := asMap(item)
		if !ok {
			return nil, fmt.Errorf("route %d must be a dict", index)
		}
		pattern, err := requireString("http_serve_routes", entry["pattern"], fmt.Sprintf("routes[%d].pattern", index))
		if err != nil {
			return nil, err
		}
		callable, ok := entry["handler"].(Callable)
		if !ok {
			return nil, fmt.Errorf("route %d must include a callable handler", index)
		}
		methods, err := parseHTTPRouteMethods(entry["methods"])
		if err != nil {
			return nil, fmt.Errorf("route %d methods: %w", index, err)
		}
		routes = append(routes, httpRoute{
			pattern: pattern,
			methods: methods,
			handler: callable,
		})
	}
	return routes, nil
}

func parseHTTPRouteMethods(value any) (map[string]struct{}, error) {
	if value == nil {
		return nil, nil
	}
	methods := make(map[string]struct{})
	switch typed := value.(type) {
	case string:
		method := strings.ToUpper(strings.TrimSpace(typed))
		if method == "" {
			return nil, fmt.Errorf("method names cannot be empty")
		}
		methods[method] = struct{}{}
	case []any:
		for _, item := range typed {
			method := strings.ToUpper(strings.TrimSpace(stringify(item)))
			if method == "" {
				return nil, fmt.Errorf("method names cannot be empty")
			}
			methods[method] = struct{}{}
		}
	default:
		return nil, fmt.Errorf("expected a string or list")
	}
	return methods, nil
}

func (r httpRoute) allowsMethod(method string) bool {
	if len(r.methods) == 0 {
		return true
	}
	_, ok := r.methods[strings.ToUpper(method)]
	return ok
}

func mergeRoutePayload(payload map[string]any, pattern string, params map[string]any, matched bool) map[string]any {
	next := make(map[string]any, len(payload)+2)
	for key, value := range payload {
		next[key] = cloneValue(value)
	}
	next["params"] = cloneValue(params)
	next["route"] = map[string]any{
		"matched": matched,
		"pattern": pattern,
		"params":  cloneValue(params),
	}
	return next
}

func allowedMethodsHeader(allowed map[string]struct{}) string {
	methods := make([]string, 0, len(allowed))
	for method := range allowed {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return strings.Join(methods, ", ")
}

func buildHTTPRequestPayload(request *http.Request) (map[string]any, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	defer request.Body.Close()

	query := make(map[string]any, len(request.URL.Query()))
	for key, values := range request.URL.Query() {
		if len(values) == 1 {
			query[key] = values[0]
			continue
		}
		items := make([]any, 0, len(values))
		for _, value := range values {
			items = append(items, value)
		}
		query[key] = items
	}

	return map[string]any{
		"method":      request.Method,
		"url":         request.URL.String(),
		"path":        request.URL.Path,
		"raw_query":   request.URL.RawQuery,
		"query":       query,
		"headers":     flattenHTTPHeaders(request.Header),
		"host":        request.Host,
		"remote_addr": request.RemoteAddr,
		"body":        string(body),
	}, nil
}

func formatHTTPHandlerResponse(value any) (int, map[string]string, string, error) {
	if responseMap, ok := asMap(value); ok {
		status := http.StatusOK
		if rawStatus, ok := responseMap["status"]; ok {
			parsedStatus, parsedOK := asInt(rawStatus)
			if !parsedOK {
				return 0, nil, "", fmt.Errorf("http handler response field status must be an integer")
			}
			status = int(parsedStatus)
		}

		headers := make(map[string]string)
		if rawHeaders, ok := responseMap["headers"]; ok {
			parsedHeaders, err := requireStringMap("http handler response", rawHeaders, "headers")
			if err != nil {
				return 0, nil, "", err
			}
			headers = canonicalizeHTTPHeaderMap(parsedHeaders)
		}

		if rawBody, ok := responseMap["body"]; ok {
			return status, headers, stringify(rawBody), nil
		}
		if rawHTML, ok := responseMap["html"]; ok {
			setDefaultHTTPHeader(headers, "Content-Type", "text/html; charset=utf-8")
			return status, headers, stringify(rawHTML), nil
		}
		if rawJSON, ok := responseMap["json"]; ok {
			setDefaultHTTPHeader(headers, "Content-Type", "application/json")
			encoded, err := json.Marshal(normalizeJSONValue(rawJSON))
			if err != nil {
				return 0, nil, "", err
			}
			return status, headers, string(encoded), nil
		}
	}

	return http.StatusOK, map[string]string{"Content-Type": "text/plain; charset=utf-8"}, stringify(value), nil
}

func routeMatch(pattern, requestPath string) (bool, map[string]any) {
	patternSegments := splitRouteSegments(pattern)
	pathSegments := splitRouteSegments(requestPath)
	params := make(map[string]any)

	patternIndex := 0
	pathIndex := 0
	for patternIndex < len(patternSegments) {
		segment := patternSegments[patternIndex]
		if strings.HasPrefix(segment, "*") {
			name := strings.TrimPrefix(segment, "*")
			if name == "" || patternIndex != len(patternSegments)-1 {
				return false, map[string]any{}
			}
			params[name] = decodeRouteValue(strings.Join(pathSegments[pathIndex:], "/"))
			return true, params
		}

		if pathIndex >= len(pathSegments) {
			return false, map[string]any{}
		}

		value := decodeRouteValue(pathSegments[pathIndex])
		if strings.HasPrefix(segment, ":") {
			name := strings.TrimPrefix(segment, ":")
			if name == "" {
				return false, map[string]any{}
			}
			params[name] = value
			patternIndex++
			pathIndex++
			continue
		}

		if segment != value {
			return false, map[string]any{}
		}
		patternIndex++
		pathIndex++
	}

	if pathIndex != len(pathSegments) {
		return false, map[string]any{}
	}
	return true, params
}

func splitRouteSegments(raw string) []string {
	cleaned := path.Clean(strings.TrimSpace(raw))
	if cleaned == "." || cleaned == "/" {
		return []string{}
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	trimmed := strings.Trim(cleaned, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

func decodeRouteValue(value string) string {
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}
