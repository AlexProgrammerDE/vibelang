package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
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

type httpResponsePayload struct {
	Status  int
	Headers map[string]string
	Body    string
	SSE     *httpSSEPayload
}

type httpSSEPayload struct {
	Events  []sseEvent
	Channel *channelHandle
}

type sseEvent struct {
	Event   string
	Data    string
	ID      string
	RetryMS int64
}

func registerHTTPServerBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("route_match", builtinRouteMatch, "dict{matched: bool, params: dict[string, string]}", "Match a request path against a route pattern like /users/:id or /assets/*path and return matched plus any extracted params.", ast.Param{Name: "pattern", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "request_path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("mime_type", builtinMimeType, "string", "Guess the HTTP content type for a file path, including application/wasm for WebAssembly modules.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "sse_event",
		call: builtinSSEEvent,
		tool: &ToolSpec{
			Name:       "sse_event",
			ReturnType: ast.TypeRef{Expr: "dict{data: string, event: string, id: string, retry_ms: int}"},
			Body:       "Build one Server-Sent Event record with text data and optional event metadata.",
			Params: []ast.Param{
				{Name: "data", Type: ast.TypeRef{Expr: "string"}},
				{Name: "event", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"message\""},
				{Name: "id", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"\""},
				{Name: "retry_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "0"},
			},
		},
		defaults: map[string]any{
			"event":    "message",
			"id":       "",
			"retry_ms": int64(0),
		},
		bindArgs:   true,
		promptSafe: true,
	})
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
		name: "http_static_response",
		call: builtinHTTPStaticResponse,
		tool: &ToolSpec{
			Name:       "http_static_response",
			ReturnType: ast.TypeRef{Expr: "dict{status: int, headers: dict[string, string], body: string}"},
			Body:       "Serve one static file from a directory using request.path. Prevent directory traversal, infer Content-Type including application/wasm, and fall back to index_file for directories.",
			Params: []ast.Param{
				{Name: "root", Type: ast.TypeRef{Expr: "string"}},
				{Name: "request", Type: ast.TypeRef{Expr: "dict"}},
				{Name: "index_file", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"index.html\""},
				{Name: "headers", Type: ast.TypeRef{Expr: "dict[string, string]"}, DefaultText: "{}"},
				{Name: "cache_control", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"\""},
			},
		},
		defaults: map[string]any{
			"index_file":    "index.html",
			"headers":       map[string]any{},
			"cache_control": "",
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

func builtinMimeType(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("mime_type", args, 1); err != nil {
		return nil, err
	}
	filePath, err := requireString("mime_type", args[0], "path")
	if err != nil {
		return nil, err
	}
	return detectStaticContentType(filePath, nil), nil
}

func builtinSSEEvent(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("sse_event", args, 4); err != nil {
		return nil, err
	}
	data, err := requireString("sse_event", args[0], "data")
	if err != nil {
		return nil, err
	}
	event, err := requireString("sse_event", args[1], "event")
	if err != nil {
		return nil, err
	}
	id, err := requireString("sse_event", args[2], "id")
	if err != nil {
		return nil, err
	}
	retryMS, err := requireInt("sse_event", args[3], "retry_ms")
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"data":     data,
		"event":    event,
		"id":       id,
		"retry_ms": retryMS,
	}, nil
}

func builtinHTTPStaticResponse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("http_static_response", args, 5); err != nil {
		return nil, err
	}
	root, err := requireString("http_static_response", args[0], "root")
	if err != nil {
		return nil, err
	}
	request, ok := asMap(args[1])
	if !ok {
		return nil, fmt.Errorf("http_static_response expects request to be a dict")
	}
	indexFile, err := requireString("http_static_response", args[2], "index_file")
	if err != nil {
		return nil, err
	}
	headers, err := requireStringMap("http_static_response", args[3], "headers")
	if err != nil {
		return nil, err
	}
	cacheControl, err := requireString("http_static_response", args[4], "cache_control")
	if err != nil {
		return nil, err
	}
	requestPath, err := requireString("http_static_response", request["path"], "request.path")
	if err != nil {
		return nil, err
	}
	return buildHTTPStaticResponse(root, requestPath, indexFile, headers, cacheControl)
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

func buildHTTPStaticResponse(root, requestPath, indexFile string, headers map[string]string, cacheControl string) (map[string]any, error) {
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	target, err := resolveStaticAssetPath(resolvedRoot, requestPath, indexFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return staticErrorResponse(http.StatusNotFound, "not found"), nil
		}
		return nil, err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return staticErrorResponse(http.StatusNotFound, "not found"), nil
		}
		return nil, err
	}

	responseHeaders := canonicalizeHTTPHeaderMap(headers)
	setDefaultHTTPHeader(responseHeaders, "Content-Type", detectStaticContentType(target, body))
	if cacheControl != "" {
		setDefaultHTTPHeader(responseHeaders, "Cache-Control", cacheControl)
	}
	return map[string]any{
		"status":  int64(http.StatusOK),
		"headers": anyHTTPHeaderMap(responseHeaders),
		"body":    string(body),
	}, nil
}

func resolveStaticAssetPath(root, requestPath, indexFile string) (string, error) {
	cleanRoot := filepath.Clean(root)
	cleanRequestPath := path.Clean("/" + strings.TrimSpace(requestPath))
	relative := strings.TrimPrefix(cleanRequestPath, "/")
	target := filepath.Join(cleanRoot, filepath.FromSlash(relative))

	if cleanRequestPath == "/" || strings.HasSuffix(requestPath, "/") {
		target = filepath.Join(target, indexFile)
	}

	resolvedTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if !isWithinStaticRoot(cleanRoot, resolvedTarget) {
		return "", os.ErrNotExist
	}

	info, err := os.Stat(resolvedTarget)
	if err == nil && info.IsDir() {
		resolvedTarget = filepath.Join(resolvedTarget, indexFile)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if !isWithinStaticRoot(cleanRoot, resolvedTarget) {
		return "", os.ErrNotExist
	}
	return resolvedTarget, nil
}

func isWithinStaticRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(os.PathSeparator))
}

func detectStaticContentType(filePath string, body []byte) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	}

	guessed := mime.TypeByExtension(strings.ToLower(filepath.Ext(filePath)))
	if guessed != "" {
		if strings.HasPrefix(guessed, "text/") && !strings.Contains(guessed, "charset=") {
			return guessed + "; charset=utf-8"
		}
		return guessed
	}
	if len(body) > 0 {
		sample := body
		if len(sample) > 512 {
			sample = sample[:512]
		}
		return http.DetectContentType(sample)
	}
	return "application/octet-stream"
}

func staticErrorResponse(status int, body string) map[string]any {
	return map[string]any{
		"status": int64(status),
		"headers": map[string]any{
			"Content-Type": "text/plain; charset=utf-8",
		},
		"body": body,
	}
}

func anyHTTPHeaderMap(headers map[string]string) map[string]any {
	converted := make(map[string]any, len(headers))
	for key, value := range headers {
		converted[key] = value
	}
	return converted
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

	response, err := i.formatHTTPHandlerResponse(result)
	if err != nil {
		i.incrementMetric("http_request_errors_total", 1)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	i.writeFormattedHTTPResponse(request.Context(), writer, response)
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
		response, err := i.formatHTTPHandlerResponse(result)
		if err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		i.writeFormattedHTTPResponse(request.Context(), writer, response)
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
		response, err := i.formatHTTPHandlerResponse(result)
		if err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		i.writeFormattedHTTPResponse(request.Context(), writer, response)
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

func (i *Interpreter) writeFormattedHTTPResponse(ctx context.Context, writer http.ResponseWriter, response httpResponsePayload) {
	if response.SSE != nil {
		i.writeHTTPEventStream(ctx, writer, response)
		return
	}
	i.writeHTTPResponse(writer, response.Status, response.Headers, response.Body)
}

func (i *Interpreter) writeHTTPEventStream(ctx context.Context, writer http.ResponseWriter, response httpResponsePayload) {
	for key, value := range response.Headers {
		writer.Header().Set(key, value)
	}
	writer.WriteHeader(response.Status)

	flusher, _ := writer.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	for _, event := range response.SSE.Events {
		if err := writeSSEEvent(writer, event); err != nil {
			i.incrementMetric("http_request_errors_total", 1)
			return
		}
		i.incrementMetric("http_sse_events_total", 1)
		flush()
	}

	if response.SSE.Channel != nil {
		i.incrementMetric("http_sse_streams_total", 1)
		for {
			select {
			case <-ctx.Done():
				i.incrementMetric("http_responses_total", 1)
				return
			case value, ok := <-response.SSE.Channel.ch:
				if !ok {
					i.incrementMetric("http_responses_total", 1)
					return
				}
				event, err := parseSSEEvent(cloneValue(value))
				if err != nil {
					i.incrementMetric("http_request_errors_total", 1)
					i.tracef("http SSE stream encode failed: %v", err)
					return
				}
				if err := writeSSEEvent(writer, event); err != nil {
					i.incrementMetric("http_request_errors_total", 1)
					return
				}
				i.incrementMetric("http_sse_events_total", 1)
				flush()
			}
		}
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

func (i *Interpreter) formatHTTPHandlerResponse(value any) (httpResponsePayload, error) {
	if responseMap, ok := asMap(value); ok {
		status := http.StatusOK
		if rawStatus, ok := responseMap["status"]; ok {
			parsedStatus, parsedOK := asInt(rawStatus)
			if !parsedOK {
				return httpResponsePayload{}, fmt.Errorf("http handler response field status must be an integer")
			}
			status = int(parsedStatus)
		}

		headers := make(map[string]string)
		if rawHeaders, ok := responseMap["headers"]; ok {
			parsedHeaders, err := requireStringMap("http handler response", rawHeaders, "headers")
			if err != nil {
				return httpResponsePayload{}, err
			}
			headers = canonicalizeHTTPHeaderMap(parsedHeaders)
		}

		bodyModes := 0
		for _, key := range []string{"body", "html", "json", "sse", "sse_channel"} {
			if _, ok := responseMap[key]; ok {
				bodyModes++
			}
		}
		if bodyModes > 1 {
			return httpResponsePayload{}, fmt.Errorf("http handler response may only include one of body, html, json, sse, or sse_channel")
		}

		if rawBody, ok := responseMap["body"]; ok {
			return httpResponsePayload{Status: status, Headers: headers, Body: stringify(rawBody)}, nil
		}
		if rawHTML, ok := responseMap["html"]; ok {
			setDefaultHTTPHeader(headers, "Content-Type", "text/html; charset=utf-8")
			return httpResponsePayload{Status: status, Headers: headers, Body: stringify(rawHTML)}, nil
		}
		if rawJSON, ok := responseMap["json"]; ok {
			setDefaultHTTPHeader(headers, "Content-Type", "application/json")
			encoded, err := json.Marshal(normalizeJSONValue(rawJSON))
			if err != nil {
				return httpResponsePayload{}, err
			}
			return httpResponsePayload{Status: status, Headers: headers, Body: string(encoded)}, nil
		}
		if rawSSE, ok := responseMap["sse"]; ok {
			events, err := parseSSEEvents(rawSSE)
			if err != nil {
				return httpResponsePayload{}, err
			}
			applySSEHeaders(headers)
			return httpResponsePayload{
				Status:  status,
				Headers: headers,
				SSE: &httpSSEPayload{
					Events: events,
				},
			}, nil
		}
		if rawChannel, ok := responseMap["sse_channel"]; ok {
			channelHandle, err := requireString("http handler response", rawChannel, "sse_channel")
			if err != nil {
				return httpResponsePayload{}, err
			}
			channel, err := i.lookupChannel(channelHandle)
			if err != nil {
				return httpResponsePayload{}, err
			}
			applySSEHeaders(headers)
			return httpResponsePayload{
				Status:  status,
				Headers: headers,
				SSE: &httpSSEPayload{
					Channel: channel,
				},
			}, nil
		}
	}

	return httpResponsePayload{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"},
		Body:    stringify(value),
	}, nil
}

func parseSSEEvents(value any) ([]sseEvent, error) {
	if items, ok := asList(value); ok {
		events := make([]sseEvent, 0, len(items))
		for index, item := range items {
			event, err := parseSSEEvent(item)
			if err != nil {
				return nil, fmt.Errorf("http handler response sse[%d]: %w", index, err)
			}
			events = append(events, event)
		}
		return events, nil
	}

	event, err := parseSSEEvent(value)
	if err != nil {
		return nil, fmt.Errorf("http handler response sse: %w", err)
	}
	return []sseEvent{event}, nil
}

func parseSSEEvent(value any) (sseEvent, error) {
	switch typed := value.(type) {
	case string:
		return sseEvent{Data: typed}, nil
	case map[string]any:
		rawData, ok := typed["data"]
		if !ok {
			return sseEvent{}, fmt.Errorf("event dict must include data")
		}
		event := sseEvent{Data: stringify(rawData)}
		if rawEvent, ok := typed["event"]; ok {
			parsedEvent, err := requireString("sse event", rawEvent, "event")
			if err != nil {
				return sseEvent{}, err
			}
			event.Event = parsedEvent
		}
		if rawID, ok := typed["id"]; ok {
			parsedID, err := requireString("sse event", rawID, "id")
			if err != nil {
				return sseEvent{}, err
			}
			event.ID = parsedID
		}
		if rawRetry, ok := typed["retry_ms"]; ok {
			retryMS, err := requireInt("sse event", rawRetry, "retry_ms")
			if err != nil {
				return sseEvent{}, err
			}
			event.RetryMS = retryMS
		}
		return event, nil
	default:
		return sseEvent{}, fmt.Errorf("expected an SSE string or dict, got %s", typeName(value))
	}
}

func applySSEHeaders(headers map[string]string) {
	setDefaultHTTPHeader(headers, "Content-Type", "text/event-stream; charset=utf-8")
	setDefaultHTTPHeader(headers, "Cache-Control", "no-cache")
	setDefaultHTTPHeader(headers, "Connection", "keep-alive")
}

func writeSSEEvent(writer io.Writer, event sseEvent) error {
	if event.Event != "" {
		if _, err := io.WriteString(writer, "event: "+sanitizeSSEField(event.Event)+"\n"); err != nil {
			return err
		}
	}
	if event.ID != "" {
		if _, err := io.WriteString(writer, "id: "+sanitizeSSEField(event.ID)+"\n"); err != nil {
			return err
		}
	}
	if event.RetryMS > 0 {
		if _, err := io.WriteString(writer, fmt.Sprintf("retry: %d\n", event.RetryMS)); err != nil {
			return err
		}
	}

	lines := strings.Split(strings.ReplaceAll(event.Data, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	for _, line := range lines {
		if _, err := io.WriteString(writer, "data: "+sanitizeSSEField(line)+"\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func sanitizeSSEField(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", "")
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
