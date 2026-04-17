package runtime

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"vibelang/internal/ast"
)

func registerConcurrencyBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, &builtinFunction{
		name: "spawn",
		call: builtinSpawn,
		tool: &ToolSpec{
			Name:       "spawn",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Run a builtin or AI function concurrently and return a task handle.",
			Params: []ast.Param{
				{Name: "callable"},
				{Name: "args", Type: ast.TypeRef{Expr: "list"}, DefaultText: "[]"},
				{Name: "kwargs", Type: ast.TypeRef{Expr: "dict"}, DefaultText: "{}"},
				{Name: "wait_group", DefaultText: "none"},
			},
		},
		defaults: map[string]any{
			"args":       []any{},
			"kwargs":     map[string]any{},
			"wait_group": nil,
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "await_task",
		call: builtinAwaitTask,
		tool: &ToolSpec{
			Name:       "await_task",
			ReturnType: ast.TypeRef{Expr: "any"},
			Body:       "Wait for a spawned task and return its result.",
			Params: []ast.Param{
				{Name: "task", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("task_status", builtinTaskStatus, "dict", "Return a dictionary describing a spawned task.", ast.Param{Name: "task", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "channel",
		call: builtinChannel,
		tool: &ToolSpec{
			Name:       "channel",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Create a channel with the given buffer capacity and return its handle.",
			Params: []ast.Param{
				{Name: "capacity", Type: ast.TypeRef{Expr: "int"}, DefaultText: "0"},
			},
		},
		defaults: map[string]any{
			"capacity": int64(0),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "channel_send",
		call: builtinChannelSend,
		tool: &ToolSpec{
			Name:       "channel_send",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "Send a value to a channel. Return false when the send timed out.",
			Params: []ast.Param{
				{Name: "channel", Type: ast.TypeRef{Expr: "string"}},
				{Name: "value"},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "channel_recv",
		call: builtinChannelRecv,
		tool: &ToolSpec{
			Name:       "channel_recv",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Receive a value from a channel and return a dict with value, ok, and timeout fields.",
			Params: []ast.Param{
				{Name: "channel", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "channel_select",
		call: builtinChannelSelect,
		tool: &ToolSpec{
			Name:       "channel_select",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Wait on multiple channels and return a dict with channel, value, ok, closed, and timeout fields.",
			Params: []ast.Param{
				{Name: "channels", Type: ast.TypeRef{Expr: "list[string]"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, toolBuiltin("channel_close", builtinChannelClose, "bool", "Close a channel handle. Return true only the first time it is closed.", ast.Param{Name: "channel", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("wait_group", builtinWaitGroup, "string", "Create a wait group handle."))
	registerBuiltin(interpreter, &builtinFunction{
		name: "wait_group_add",
		call: builtinWaitGroupAdd,
		tool: &ToolSpec{
			Name:       "wait_group_add",
			ReturnType: ast.TypeRef{Expr: "int"},
			Body:       "Add delta to a wait group counter and return the new counter.",
			Params: []ast.Param{
				{Name: "wait_group", Type: ast.TypeRef{Expr: "string"}},
				{Name: "delta", Type: ast.TypeRef{Expr: "int"}, DefaultText: "1"},
			},
		},
		defaults: map[string]any{
			"delta": int64(1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, toolBuiltin("wait_group_done", builtinWaitGroupDone, "int", "Decrement a wait group counter and return the new counter.", ast.Param{Name: "wait_group", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "wait_group_wait",
		call: builtinWaitGroupWait,
		tool: &ToolSpec{
			Name:       "wait_group_wait",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "Wait for a wait group to reach zero. Return false when the wait timed out.",
			Params: []ast.Param{
				{Name: "wait_group", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("mutex", builtinMutex, "string", "Create a mutex handle."))
	registerBuiltin(interpreter, &builtinFunction{
		name: "mutex_guard",
		call: builtinMutexGuard,
		tool: &ToolSpec{
			Name:       "mutex_guard",
			ReturnType: ast.TypeRef{Expr: "context"},
			Body:       "Create a mutex guard context for use in a with block.",
			Params: []ast.Param{
				{Name: "mutex", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs:   true,
		hiddenTool: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "mutex_lock",
		call: builtinMutexLock,
		tool: &ToolSpec{
			Name:       "mutex_lock",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "Acquire a mutex. Return false when the lock timed out.",
			Params: []ast.Param{
				{Name: "mutex", Type: ast.TypeRef{Expr: "string"}},
				{Name: "timeout_ms", Type: ast.TypeRef{Expr: "int"}, DefaultText: "-1"},
			},
		},
		defaults: map[string]any{
			"timeout_ms": int64(-1),
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, toolBuiltin("mutex_unlock", builtinMutexUnlock, "bool", "Release a mutex handle.", ast.Param{Name: "mutex", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("metrics_snapshot", builtinMetricsSnapshot, "dict[string, int]", "Return the current interpreter metrics counters."))
}

func builtinSpawn(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("spawn", args, 4); err != nil {
		return nil, err
	}

	callable, ok := args[0].(Callable)
	if !ok {
		return nil, fmt.Errorf("spawn expects callable to be a function")
	}

	positional, ok := asList(args[1])
	if !ok {
		return nil, fmt.Errorf("spawn expects args to be a list")
	}

	keywords, ok := asMap(args[2])
	if !ok {
		return nil, fmt.Errorf("spawn expects kwargs to be a dict")
	}

	var group *safeWaitGroup
	if args[3] != nil {
		waitGroupHandle, err := requireString("spawn", args[3], "wait_group")
		if err != nil {
			return nil, err
		}
		group, err = interpreter.lookupWaitGroup(waitGroupHandle)
		if err != nil {
			return nil, err
		}
	}

	callArgs := make([]CallArgument, 0, len(positional)+len(keywords))
	for _, value := range positional {
		callArgs = append(callArgs, CallArgument{Value: cloneValue(value)})
	}
	for name, value := range keywords {
		callArgs = append(callArgs, CallArgument{Name: name, Value: cloneValue(value)})
	}

	task := newTaskHandle()
	handleID := interpreter.nextHandle("task")
	interpreter.storeTask(handleID, task)
	interpreter.incrementMetric("tasks_spawned_total", 1)

	go func() {
		if group != nil {
			defer group.Done()
		}

		result, err := callable.Call(context.Background(), interpreter, callArgs)
		if err != nil {
			interpreter.incrementMetric("tasks_failed_total", 1)
		} else {
			interpreter.incrementMetric("tasks_completed_total", 1)
		}
		task.complete(result, err)
	}()

	return handleID, nil
}

func builtinAwaitTask(ctx context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("await_task", args, 2); err != nil {
		return nil, err
	}
	taskHandle, err := requireString("await_task", args[0], "task")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("await_task", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}

	result, timedOut, err := interpreter.awaitTask(ctx, taskHandle, timeoutMS)
	if err != nil {
		return nil, err
	}
	if timedOut {
		return nil, fmt.Errorf("await_task timed out after %dms", timeoutMS)
	}
	return result, nil
}

func builtinTaskStatus(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("task_status", args, 1); err != nil {
		return nil, err
	}
	taskHandle, err := requireString("task_status", args[0], "task")
	if err != nil {
		return nil, err
	}
	task, err := interpreter.lookupTask(taskHandle)
	if err != nil {
		return nil, err
	}
	return task.snapshot(), nil
}

func builtinChannel(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("channel", args, 1); err != nil {
		return nil, err
	}
	capacity, err := requireInt("channel", args[0], "capacity")
	if err != nil {
		return nil, err
	}
	if capacity < 0 {
		return nil, fmt.Errorf("channel capacity cannot be negative")
	}

	handleID := interpreter.nextHandle("channel")
	interpreter.storeChannel(handleID, newChannelHandle(int(capacity)))
	return handleID, nil
}

func builtinChannelSend(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("channel_send", args, 3); err != nil {
		return nil, err
	}
	channelHandle, err := requireString("channel_send", args[0], "channel")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("channel_send", args[2], "timeout_ms")
	if err != nil {
		return nil, err
	}
	channel, err := interpreter.lookupChannel(channelHandle)
	if err != nil {
		return nil, err
	}
	sent, err := channel.Send(args[1], waitTimeout(timeoutMS))
	if err != nil {
		return nil, err
	}
	return sent, nil
}

func builtinChannelRecv(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("channel_recv", args, 2); err != nil {
		return nil, err
	}
	channelHandle, err := requireString("channel_recv", args[0], "channel")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("channel_recv", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}
	channel, err := interpreter.lookupChannel(channelHandle)
	if err != nil {
		return nil, err
	}
	value, ok, timedOut := channel.Recv(waitTimeout(timeoutMS))
	return map[string]any{
		"value":   value,
		"ok":      ok,
		"timeout": timedOut,
	}, nil
}

func builtinChannelSelect(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("channel_select", args, 2); err != nil {
		return nil, err
	}
	rawChannels, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("channel_select expects channels to be a list")
	}
	if len(rawChannels) == 0 {
		return nil, fmt.Errorf("channel_select expects at least one channel")
	}
	timeoutMS, err := requireInt("channel_select", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}

	cases := make([]reflect.SelectCase, 0, len(rawChannels)+1)
	channelIDs := make([]string, 0, len(rawChannels))
	for _, raw := range rawChannels {
		channelID, err := requireString("channel_select", raw, "channels")
		if err != nil {
			return nil, err
		}
		channel, err := interpreter.lookupChannel(channelID)
		if err != nil {
			return nil, err
		}
		channelIDs = append(channelIDs, channelID)
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(channel.ch),
		})
	}

	timeoutIndex := -1
	var timer *time.Timer
	if timeoutMS >= 0 {
		timer = time.NewTimer(waitTimeout(timeoutMS))
		defer timer.Stop()
		timeoutIndex = len(cases)
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(timer.C),
		})
	}

	chosen, value, ok := reflect.Select(cases)
	interpreter.incrementMetric("channel_selects_total", 1)
	if timeoutIndex >= 0 && chosen == timeoutIndex {
		return map[string]any{
			"channel": nil,
			"value":   nil,
			"ok":      false,
			"closed":  false,
			"timeout": true,
		}, nil
	}

	if !ok {
		return map[string]any{
			"channel": channelIDs[chosen],
			"value":   nil,
			"ok":      false,
			"closed":  true,
			"timeout": false,
		}, nil
	}

	return map[string]any{
		"channel": channelIDs[chosen],
		"value":   cloneValue(value.Interface()),
		"ok":      true,
		"closed":  false,
		"timeout": false,
	}, nil
}

func builtinChannelClose(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("channel_close", args, 1); err != nil {
		return nil, err
	}
	channelHandle, err := requireString("channel_close", args[0], "channel")
	if err != nil {
		return nil, err
	}
	return interpreter.closeChannel(channelHandle)
}

func builtinWaitGroup(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("wait_group", args, 0); err != nil {
		return nil, err
	}
	handleID := interpreter.nextHandle("wait_group")
	interpreter.storeWaitGroup(handleID, newSafeWaitGroup())
	return handleID, nil
}

func builtinWaitGroupAdd(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("wait_group_add", args, 2); err != nil {
		return nil, err
	}
	waitGroupHandle, err := requireString("wait_group_add", args[0], "wait_group")
	if err != nil {
		return nil, err
	}
	delta, err := requireInt("wait_group_add", args[1], "delta")
	if err != nil {
		return nil, err
	}
	waitGroup, err := interpreter.lookupWaitGroup(waitGroupHandle)
	if err != nil {
		return nil, err
	}
	counter, err := waitGroup.Add(int(delta))
	if err != nil {
		return nil, err
	}
	return int64(counter), nil
}

func builtinWaitGroupDone(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("wait_group_done", args, 1); err != nil {
		return nil, err
	}
	waitGroupHandle, err := requireString("wait_group_done", args[0], "wait_group")
	if err != nil {
		return nil, err
	}
	waitGroup, err := interpreter.lookupWaitGroup(waitGroupHandle)
	if err != nil {
		return nil, err
	}
	counter, err := waitGroup.Done()
	if err != nil {
		return nil, err
	}
	return int64(counter), nil
}

func builtinWaitGroupWait(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("wait_group_wait", args, 2); err != nil {
		return nil, err
	}
	waitGroupHandle, err := requireString("wait_group_wait", args[0], "wait_group")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("wait_group_wait", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}
	waitGroup, err := interpreter.lookupWaitGroup(waitGroupHandle)
	if err != nil {
		return nil, err
	}
	return waitGroup.Wait(waitTimeout(timeoutMS)), nil
}

func builtinMutex(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("mutex", args, 0); err != nil {
		return nil, err
	}
	handleID := interpreter.nextHandle("mutex")
	interpreter.storeMutex(handleID, newMutexHandle())
	return handleID, nil
}

func builtinMutexGuard(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("mutex_guard", args, 2); err != nil {
		return nil, err
	}
	mutexHandle, err := requireString("mutex_guard", args[0], "mutex")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("mutex_guard", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}

	return newManagedContext("mutex_guard", func(_ context.Context, interpreter *Interpreter) (any, error) {
		lock, err := interpreter.lookupMutex(mutexHandle)
		if err != nil {
			return nil, err
		}
		if !lock.Lock(waitTimeout(timeoutMS)) {
			return nil, fmt.Errorf("mutex_guard timed out after %dms", timeoutMS)
		}
		return mutexHandle, nil
	}, func(_ context.Context, interpreter *Interpreter, _ error) error {
		lock, err := interpreter.lookupMutex(mutexHandle)
		if err != nil {
			return err
		}
		return lock.Unlock()
	}), nil
}

func builtinMutexLock(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("mutex_lock", args, 2); err != nil {
		return nil, err
	}
	mutexHandle, err := requireString("mutex_lock", args[0], "mutex")
	if err != nil {
		return nil, err
	}
	timeoutMS, err := requireInt("mutex_lock", args[1], "timeout_ms")
	if err != nil {
		return nil, err
	}
	mutex, err := interpreter.lookupMutex(mutexHandle)
	if err != nil {
		return nil, err
	}
	return mutex.Lock(waitTimeout(timeoutMS)), nil
}

func builtinMutexUnlock(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("mutex_unlock", args, 1); err != nil {
		return nil, err
	}
	mutexHandle, err := requireString("mutex_unlock", args[0], "mutex")
	if err != nil {
		return nil, err
	}
	mutex, err := interpreter.lookupMutex(mutexHandle)
	if err != nil {
		return nil, err
	}
	if err := mutex.Unlock(); err != nil {
		return nil, err
	}
	return true, nil
}

func builtinMetricsSnapshot(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("metrics_snapshot", args, 0); err != nil {
		return nil, err
	}
	return interpreter.metricsSnapshot(), nil
}
