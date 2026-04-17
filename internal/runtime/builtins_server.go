package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"vibelang/internal/ast"
)

func registerHTTPServerBuiltins(interpreter *Interpreter) {
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

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	serverHandle := interpreter.nextHandle("http_server")
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			interpreter.serveAIHTTP(writer, request, handler)
		}),
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
			headers = parsedHeaders
		}

		if rawBody, ok := responseMap["body"]; ok {
			return status, headers, stringify(rawBody), nil
		}
		if rawHTML, ok := responseMap["html"]; ok {
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "text/html; charset=utf-8"
			}
			return status, headers, stringify(rawHTML), nil
		}
		if rawJSON, ok := responseMap["json"]; ok {
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
			encoded, err := json.Marshal(normalizeJSONValue(rawJSON))
			if err != nil {
				return 0, nil, "", err
			}
			return status, headers, string(encoded), nil
		}
	}

	return http.StatusOK, map[string]string{"Content-Type": "text/plain; charset=utf-8"}, stringify(value), nil
}
