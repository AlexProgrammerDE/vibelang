package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vibelang/internal/ast"
)

type socketHandle struct {
	conn net.Conn
}

func registerExtendedBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("glob", builtinGlob, "list[string]", "Return the sorted filesystem matches for a glob pattern.", ast.Param{Name: "pattern", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("copy_file", builtinCopyFile, "string", "Copy a file to a new path, creating parent directories when needed. Return the destination path.", ast.Param{Name: "source", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "destination", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("move_file", builtinMoveFile, "string", "Move or rename a file to a new path, creating parent directories when needed. Return the destination path.", ast.Param{Name: "source", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "destination", Type: ast.TypeRef{Expr: "string"}}))

	registerBuiltin(interpreter, promptToolBuiltin("sqrt", builtinSqrt, "float", "Return the square root of a number.", ast.Param{Name: "value", Type: ast.TypeRef{Expr: "float"}}))
	registerBuiltin(interpreter, promptToolBuiltin("pow", builtinPow, "float", "Raise base to exponent and return the numeric result.", ast.Param{Name: "base", Type: ast.TypeRef{Expr: "float"}}, ast.Param{Name: "exponent", Type: ast.TypeRef{Expr: "float"}}))
	registerBuiltin(interpreter, promptToolBuiltin("abs", builtinAbs, "float", "Return the absolute value of a number.", ast.Param{Name: "value", Type: ast.TypeRef{Expr: "float"}}))
	registerBuiltin(interpreter, promptToolBuiltin("floor", builtinFloor, "int", "Return the floor of a number as an integer.", ast.Param{Name: "value", Type: ast.TypeRef{Expr: "float"}}))
	registerBuiltin(interpreter, promptToolBuiltin("ceil", builtinCeil, "int", "Return the ceiling of a number as an integer.", ast.Param{Name: "value", Type: ast.TypeRef{Expr: "float"}}))

	registerBuiltin(interpreter, promptToolBuiltin("now", builtinNow, "string", "Return the current time in RFC3339 format."))
	registerBuiltin(interpreter, promptToolBuiltin("unix_time", builtinUnixTime, "int", "Return the current Unix timestamp in seconds."))
	registerBuiltin(interpreter, &builtinFunction{
		name: "sleep",
		call: builtinSleep,
		tool: &ToolSpec{
			Name:       "sleep",
			ReturnType: ast.TypeRef{Expr: "none"},
			Body:       "Pause execution for the given number of milliseconds.",
			Params: []ast.Param{
				{Name: "milliseconds", Type: ast.TypeRef{Expr: "int"}},
			},
		},
	})

	registerBuiltin(interpreter, &builtinFunction{
		name: "http_request",
		call: builtinHTTPRequest,
		tool: &ToolSpec{
			Name:       "http_request",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Perform an HTTP request and return a dictionary with status, status_text, headers, and body.",
			Params: []ast.Param{
				{Name: "url", Type: ast.TypeRef{Expr: "string"}},
				{Name: "method", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"GET\""},
				{Name: "body", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"\""},
				{Name: "headers", Type: ast.TypeRef{Expr: "dict[string, string]"}, DefaultText: "{}"},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "10000"},
			},
		},
		defaults: map[string]any{
			"method":     "GET",
			"body":       "",
			"headers":    map[string]any{},
			"timeout_ms": int64(10000),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "http_request_json",
		call: builtinHTTPRequestJSON,
		tool: &ToolSpec{
			Name:       "http_request_json",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Perform an HTTP request with an optional JSON body and decode the JSON response into a json field.",
			Params: []ast.Param{
				{Name: "url", Type: ast.TypeRef{Expr: "string"}},
				{Name: "method", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"GET\""},
				{Name: "body", DefaultText: "none"},
				{Name: "headers", Type: ast.TypeRef{Expr: "dict[string, string]"}, DefaultText: "{}"},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "10000"},
			},
		},
		defaults: map[string]any{
			"method":     "GET",
			"body":       nil,
			"headers":    map[string]any{},
			"timeout_ms": int64(10000),
		},
		bindArgs: true,
	})

	registerBuiltin(interpreter, &builtinFunction{
		name: "run_process",
		call: builtinRunProcess,
		tool: &ToolSpec{
			Name:       "run_process",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Execute a local process and return a dictionary with success, exit_code, stdout, and stderr.",
			Params: []ast.Param{
				{Name: "command", Type: ast.TypeRef{Expr: "string"}},
				{Name: "args", Type: ast.TypeRef{Expr: "list[string]"}, DefaultText: "[]"},
				{Name: "dir", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"\""},
				{Name: "input", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"\""},
				{Name: "env", Type: ast.TypeRef{Expr: "dict[string, string]"}, DefaultText: "{}"},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "30000"},
			},
		},
		defaults: map[string]any{
			"args":       []any{},
			"dir":        "",
			"input":      "",
			"env":        map[string]any{},
			"timeout_ms": int64(30000),
		},
		bindArgs: true,
	})

	registerBuiltin(interpreter, &builtinFunction{
		name: "socket_listen",
		call: builtinSocketListen,
		tool: &ToolSpec{
			Name:       "socket_listen",
			ReturnType: ast.TypeRef{Expr: "dict{handle: string, address: string}"},
			Body:       "Start listening for socket connections and return a dict with the listener handle and bound address.",
			Params: []ast.Param{
				{Name: "address", Type: ast.TypeRef{Expr: "string"}},
				{Name: "network", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"tcp\""},
			},
		},
		defaults: map[string]any{
			"network": "tcp",
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "socket_open",
		call: builtinSocketOpen,
		tool: &ToolSpec{
			Name:       "socket_open",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Open a socket connection and return an opaque handle string.",
			Params: []ast.Param{
				{Name: "address", Type: ast.TypeRef{Expr: "string"}},
				{Name: "network", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"tcp\""},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "5000"},
			},
		},
		defaults: map[string]any{
			"network":    "tcp",
			"timeout_ms": int64(5000),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "socket_accept",
		call: builtinSocketAccept,
		tool: &ToolSpec{
			Name:       "socket_accept",
			ReturnType: ast.TypeRef{Expr: "dict{ok: bool, timeout: bool, handle: optional[string], local_addr: string, remote_addr: string}"},
			Body:       "Accept the next connection from a socket listener handle. Return ok plus a new socket handle or timeout information.",
			Params: []ast.Param{
				{Name: "listener", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, toolBuiltin("socket_write", builtinSocketWrite, "int", "Write text to an open socket handle and return the number of bytes written.", ast.Param{Name: "handle", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "data", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "socket_read",
		call: builtinSocketRead,
		tool: &ToolSpec{
			Name:       "socket_read",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Read up to max_bytes from an open socket handle and return the received text.",
			Params: []ast.Param{
				{Name: "handle", Type: ast.TypeRef{Expr: "string"}},
				{Name: "max_bytes", Type: ast.TypeRef{Expr: "int"}, DefaultText: "4096"},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "1000"},
			},
		},
		defaults: map[string]any{
			"max_bytes":  int64(4096),
			"timeout_ms": int64(1000),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, toolBuiltin("socket_local_addr", builtinSocketLocalAddr, "string", "Return the local address for an open socket handle.", ast.Param{Name: "handle", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("socket_remote_addr", builtinSocketRemoteAddr, "string", "Return the remote address for an open socket handle.", ast.Param{Name: "handle", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("socket_listener_close", builtinSocketListenerClose, "bool", "Close a socket listener handle. Return true when a listener was closed and false when the handle was already gone.", ast.Param{Name: "listener", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("socket_close", builtinSocketClose, "bool", "Close an open socket handle. Return true when a socket was closed and false when the handle was already gone.", ast.Param{Name: "handle", Type: ast.TypeRef{Expr: "string"}}))
	registerConcurrencyBuiltins(interpreter)
	registerHTTPServerBuiltins(interpreter)
}

func builtinGlob(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("glob", args, 1); err != nil {
		return nil, err
	}
	pattern, err := requireString("glob", args[0], "pattern")
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	result := make([]any, 0, len(matches))
	for _, match := range matches {
		result = append(result, match)
	}
	return result, nil
}

func builtinCopyFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("copy_file", args, 2); err != nil {
		return nil, err
	}
	source, err := requireString("copy_file", args[0], "source")
	if err != nil {
		return nil, err
	}
	destination, err := requireString("copy_file", args[1], "destination")
	if err != nil {
		return nil, err
	}
	if err := ensureParentDir(destination); err != nil {
		return nil, err
	}
	return destination, copyFileContents(source, destination)
}

func builtinMoveFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("move_file", args, 2); err != nil {
		return nil, err
	}
	source, err := requireString("move_file", args[0], "source")
	if err != nil {
		return nil, err
	}
	destination, err := requireString("move_file", args[1], "destination")
	if err != nil {
		return nil, err
	}
	if err := ensureParentDir(destination); err != nil {
		return nil, err
	}
	if err := os.Rename(source, destination); err == nil {
		return destination, nil
	}
	if err := copyFileContents(source, destination); err != nil {
		return nil, err
	}
	if err := os.Remove(source); err != nil {
		return nil, err
	}
	return destination, nil
}

func builtinSqrt(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("sqrt", args, 1); err != nil {
		return nil, err
	}
	value, err := requireFloat("sqrt", args[0], "value")
	if err != nil {
		return nil, err
	}
	return math.Sqrt(value), nil
}

func builtinPow(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("pow", args, 2); err != nil {
		return nil, err
	}
	base, err := requireFloat("pow", args[0], "base")
	if err != nil {
		return nil, err
	}
	exponent, err := requireFloat("pow", args[1], "exponent")
	if err != nil {
		return nil, err
	}
	return math.Pow(base, exponent), nil
}

func builtinAbs(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("abs", args, 1); err != nil {
		return nil, err
	}
	if value, ok := asInt(args[0]); ok {
		if value < 0 {
			return -value, nil
		}
		return value, nil
	}
	value, err := requireFloat("abs", args[0], "value")
	if err != nil {
		return nil, err
	}
	return math.Abs(value), nil
}

func builtinFloor(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("floor", args, 1); err != nil {
		return nil, err
	}
	value, err := requireFloat("floor", args[0], "value")
	if err != nil {
		return nil, err
	}
	return int64(math.Floor(value)), nil
}

func builtinCeil(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("ceil", args, 1); err != nil {
		return nil, err
	}
	value, err := requireFloat("ceil", args[0], "value")
	if err != nil {
		return nil, err
	}
	return int64(math.Ceil(value)), nil
}

func builtinNow(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("now", args, 0); err != nil {
		return nil, err
	}
	return time.Now().Format(time.RFC3339), nil
}

func builtinUnixTime(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("unix_time", args, 0); err != nil {
		return nil, err
	}
	return time.Now().Unix(), nil
}

func builtinSleep(ctx context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("sleep", args, 1); err != nil {
		return nil, err
	}
	delayMS, err := requireInt("sleep", args[0], "milliseconds")
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, nil
	}
}

func builtinHTTPRequest(ctx context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("http_request", args, 5); err != nil {
		return nil, err
	}
	url, err := requireString("http_request", args[0], "url")
	if err != nil {
		return nil, err
	}
	method, err := requireString("http_request", args[1], "method")
	if err != nil {
		return nil, err
	}
	body, err := requireString("http_request", args[2], "body")
	if err != nil {
		return nil, err
	}
	headers, err := requireStringMap("http_request", args[3], "headers")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("http_request", args[4], "timeout_ms")
	if err != nil {
		return nil, err
	}

	return doHTTPRequest(ctx, url, method, body, headers, timeoutMS)
}

func builtinHTTPRequestJSON(ctx context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("http_request_json", args, 5); err != nil {
		return nil, err
	}
	url, err := requireString("http_request_json", args[0], "url")
	if err != nil {
		return nil, err
	}
	method, err := requireString("http_request_json", args[1], "method")
	if err != nil {
		return nil, err
	}
	headers, err := requireStringMap("http_request_json", args[3], "headers")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("http_request_json", args[4], "timeout_ms")
	if err != nil {
		return nil, err
	}

	body := ""
	if args[2] != nil {
		encoded, err := json.Marshal(normalizeJSONValue(args[2]))
		if err != nil {
			return nil, err
		}
		body = string(encoded)
		headers = canonicalizeHTTPHeaderMap(headers)
		setDefaultHTTPHeader(headers, "Content-Type", "application/json")
	}

	response, err := doHTTPRequest(ctx, url, method, body, headers, timeoutMS)
	if err != nil {
		return nil, err
	}

	responseBody, _ := response["body"].(string)
	if strings.TrimSpace(responseBody) == "" {
		response["json"] = nil
		return response, nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(responseBody), &decoded); err != nil {
		return nil, fmt.Errorf("http_request_json expected a JSON response body: %w", err)
	}
	response["json"] = normalizeJSONValue(decoded)
	return response, nil
}

func doHTTPRequest(ctx context.Context, url, method, body string, headers map[string]string, timeoutMS int64) (map[string]any, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	request, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	client := &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":      int64(response.StatusCode),
		"status_text": response.Status,
		"headers":     flattenHTTPHeaders(response.Header),
		"body":        string(responseBody),
	}, nil
}

func builtinRunProcess(ctx context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("run_process", args, 6); err != nil {
		return nil, err
	}
	commandName, err := requireString("run_process", args[0], "command")
	if err != nil {
		return nil, err
	}
	commandArgs, err := requireStringList("run_process", args[1], "args")
	if err != nil {
		return nil, err
	}
	workingDir, err := requireString("run_process", args[2], "dir")
	if err != nil {
		return nil, err
	}
	input, err := requireString("run_process", args[3], "input")
	if err != nil {
		return nil, err
	}
	extraEnv, err := requireStringMap("run_process", args[4], "env")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("run_process", args[5], "timeout_ms")
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	command := exec.CommandContext(runCtx, commandName, commandArgs...)
	if workingDir != "" {
		command.Dir = workingDir
	}
	command.Env = mergedCommandEnv(extraEnv)
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err = command.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("run_process timed out after %dms", timeoutMS)
	}

	exitCode := int64(0)
	success := err == nil
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = int64(exitErr.ExitCode())
		} else {
			return nil, err
		}
	} else if command.ProcessState != nil {
		exitCode = int64(command.ProcessState.ExitCode())
	}

	return map[string]any{
		"success":   success,
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
	}, nil
}

func builtinSocketListen(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_listen", args, 2); err != nil {
		return nil, err
	}
	address, err := requireString("socket_listen", args[0], "address")
	if err != nil {
		return nil, err
	}
	network, err := requireString("socket_listen", args[1], "network")
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	handle := interpreter.nextHandle("socket_listener")
	interpreter.storeSocketListener(handle, &socketListenerHandle{
		listener: listener,
		address:  listener.Addr().String(),
	})
	interpreter.incrementMetric("socket_listeners_started_total", 1)
	return map[string]any{
		"handle":  handle,
		"address": listener.Addr().String(),
	}, nil
}

func builtinSocketOpen(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_open", args, 3); err != nil {
		return nil, err
	}
	address, err := requireString("socket_open", args[0], "address")
	if err != nil {
		return nil, err
	}
	network, err := requireString("socket_open", args[1], "network")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("socket_open", args[2], "timeout_ms")
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, time.Duration(timeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	handle := interpreter.nextHandle("socket")
	interpreter.storeSocket(handle, &socketHandle{conn: conn})
	return handle, nil
}

func builtinSocketAccept(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_accept", args, 2); err != nil {
		return nil, err
	}
	listenerHandle, err := requireString("socket_accept", args[0], "listener")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("socket_accept", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}

	handle, err := interpreter.lookupSocketListener(listenerHandle)
	if err != nil {
		return nil, err
	}

	if timeoutMS >= 0 {
		deadlineSetter, ok := handle.listener.(interface{ SetDeadline(time.Time) error })
		if !ok {
			return nil, fmt.Errorf("socket_accept timeouts require a listener that supports deadlines")
		}
		if err := deadlineSetter.SetDeadline(time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)); err != nil {
			return nil, err
		}
		defer deadlineSetter.SetDeadline(time.Time{})
	}

	conn, err := handle.listener.Accept()
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return map[string]any{
				"ok":          false,
				"timeout":     true,
				"handle":      nil,
				"local_addr":  "",
				"remote_addr": "",
			}, nil
		}
		return nil, err
	}

	connHandle := interpreter.nextHandle("socket")
	interpreter.storeSocket(connHandle, &socketHandle{conn: conn})
	interpreter.incrementMetric("socket_connections_accepted_total", 1)
	return map[string]any{
		"ok":          true,
		"timeout":     false,
		"handle":      connHandle,
		"local_addr":  conn.LocalAddr().String(),
		"remote_addr": conn.RemoteAddr().String(),
	}, nil
}

func builtinSocketWrite(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_write", args, 2); err != nil {
		return nil, err
	}
	handleID, err := requireString("socket_write", args[0], "handle")
	if err != nil {
		return nil, err
	}
	data, err := requireString("socket_write", args[1], "data")
	if err != nil {
		return nil, err
	}
	handle, err := interpreter.lookupSocket(handleID)
	if err != nil {
		return nil, err
	}
	written, err := io.WriteString(handle.conn, data)
	if err != nil {
		return nil, err
	}
	return int64(written), nil
}

func builtinSocketRead(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_read", args, 3); err != nil {
		return nil, err
	}
	handleID, err := requireString("socket_read", args[0], "handle")
	if err != nil {
		return nil, err
	}
	maxBytes, err := requireInt("socket_read", args[1], "max_bytes")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("socket_read", args[2], "timeout_ms")
	if err != nil {
		return nil, err
	}
	handle, err := interpreter.lookupSocket(handleID)
	if err != nil {
		return nil, err
	}

	if err := handle.conn.SetReadDeadline(time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)); err != nil {
		return nil, err
	}
	defer handle.conn.SetReadDeadline(time.Time{})

	buffer := make([]byte, maxBytes)
	n, err := handle.conn.Read(buffer)
	if err != nil {
		if err == io.EOF {
			return "", nil
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return "", nil
		}
		return nil, err
	}
	return string(buffer[:n]), nil
}

func builtinSocketClose(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_close", args, 1); err != nil {
		return nil, err
	}
	handleID, err := requireString("socket_close", args[0], "handle")
	if err != nil {
		return nil, err
	}
	handle, ok := interpreter.closeSocket(handleID)
	if !ok {
		return false, nil
	}
	if err := handle.conn.Close(); err != nil {
		return nil, err
	}
	return true, nil
}

func builtinSocketLocalAddr(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_local_addr", args, 1); err != nil {
		return nil, err
	}
	handleID, err := requireString("socket_local_addr", args[0], "handle")
	if err != nil {
		return nil, err
	}
	handle, err := interpreter.lookupSocket(handleID)
	if err != nil {
		return nil, err
	}
	return handle.conn.LocalAddr().String(), nil
}

func builtinSocketRemoteAddr(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_remote_addr", args, 1); err != nil {
		return nil, err
	}
	handleID, err := requireString("socket_remote_addr", args[0], "handle")
	if err != nil {
		return nil, err
	}
	handle, err := interpreter.lookupSocket(handleID)
	if err != nil {
		return nil, err
	}
	return handle.conn.RemoteAddr().String(), nil
}

func builtinSocketListenerClose(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("socket_listener_close", args, 1); err != nil {
		return nil, err
	}
	handleID, err := requireString("socket_listener_close", args[0], "listener")
	if err != nil {
		return nil, err
	}
	handle, ok := interpreter.closeSocketListener(handleID)
	if !ok {
		return false, nil
	}
	if err := handle.listener.Close(); err != nil {
		return nil, err
	}
	interpreter.incrementMetric("socket_listeners_stopped_total", 1)
	return true, nil
}

func requireFloat(name string, value any, param string) (float64, error) {
	number, ok := asFloat(value)
	if !ok {
		return 0, fmt.Errorf("%s expects %s to be a number", name, param)
	}
	return number, nil
}

func requireInt(name string, value any, param string) (int64, error) {
	number, ok := asInt(value)
	if !ok {
		return 0, fmt.Errorf("%s expects %s to be an integer", name, param)
	}
	return number, nil
}

func requireStringMap(name string, value any, param string) (map[string]string, error) {
	dict, ok := asMap(value)
	if !ok {
		return nil, fmt.Errorf("%s expects %s to be a dict", name, param)
	}
	result := make(map[string]string, len(dict))
	for key, item := range dict {
		result[key] = stringify(item)
	}
	return result, nil
}

func canonicalizeHTTPHeaderMap(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		key = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		normalized[key] = value
	}
	return normalized
}

func setDefaultHTTPHeader(headers map[string]string, key, value string) {
	key = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))
	if key == "" {
		return
	}
	if _, exists := headers[key]; exists {
		return
	}
	headers[key] = value
}

func flattenHTTPHeaders(headers http.Header) map[string]any {
	flattened := make(map[string]any, len(headers))
	for key, values := range headers {
		flattened[key] = strings.Join(values, ", ")
	}
	return flattened
}

func mergedCommandEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	env := make([]string, 0, len(extra)+len(os.Environ()))
	base := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		base[key] = value
	}
	for key, value := range extra {
		base[key] = value
	}
	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+base[key])
	}
	return env
}

func copyFileContents(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer output.Close()

	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return nil
}
