package runtime

import (
	"context"
	"math"
	runtimemetrics "runtime/metrics"
)

func builtinRuntimeMetrics(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("runtime_metrics", args, 0); err != nil {
		return nil, err
	}
	return snapshotRuntimeMetrics(), nil
}

func builtinRuntimeMetric(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("runtime_metric", args, 2); err != nil {
		return nil, err
	}
	name, err := requireString("runtime_metric", args[0], "name")
	if err != nil {
		return nil, err
	}

	snapshot := snapshotRuntimeMetrics()
	if value, ok := snapshot[name]; ok {
		return value, nil
	}
	return cloneValue(args[1]), nil
}

func snapshotRuntimeMetrics() map[string]any {
	samples := []runtimemetrics.Sample{
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
		{Name: "/gc/gomemlimit:bytes"},
		{Name: "/gc/heap/allocs:bytes"},
		{Name: "/gc/heap/allocs:objects"},
		{Name: "/gc/heap/goal:bytes"},
		{Name: "/sched/gomaxprocs:threads"},
		{Name: "/gc/gogc:percent"},
	}
	runtimemetrics.Read(samples)

	values := make(map[string]runtimemetrics.Value, len(samples))
	for _, sample := range samples {
		values[sample.Name] = sample.Value
	}

	snapshot := make(map[string]any)
	if value, ok := readUint64Metric(values["/sched/goroutines:goroutines"]); ok {
		snapshot["go.goroutine.count"] = int64(value)
	}
	if total, ok := readUint64Metric(values["/memory/classes/total:bytes"]); ok {
		if released, ok := readUint64Metric(values["/memory/classes/heap/released:bytes"]); ok && total >= released {
			snapshot["go.memory.used"] = int64(total - released)
		}
	}
	if value, ok := readUint64Metric(values["/gc/gomemlimit:bytes"]); ok && value != uint64(math.MaxInt64) {
		snapshot["go.memory.limit"] = int64(value)
	}
	if value, ok := readUint64Metric(values["/gc/heap/allocs:bytes"]); ok {
		snapshot["go.memory.allocated"] = int64(value)
	}
	if value, ok := readUint64Metric(values["/gc/heap/allocs:objects"]); ok {
		snapshot["go.memory.allocations"] = int64(value)
	}
	if value, ok := readUint64Metric(values["/gc/heap/goal:bytes"]); ok {
		snapshot["go.memory.gc.goal"] = int64(value)
	}
	if value, ok := readUint64Metric(values["/sched/gomaxprocs:threads"]); ok {
		snapshot["go.processor.limit"] = int64(value)
	}
	if value, ok := readUint64Metric(values["/gc/gogc:percent"]); ok {
		snapshot["go.config.gogc"] = int64(value)
	}

	return snapshot
}

func readUint64Metric(value runtimemetrics.Value) (uint64, bool) {
	switch value.Kind() {
	case runtimemetrics.KindUint64:
		return value.Uint64(), true
	case runtimemetrics.KindFloat64:
		return uint64(value.Float64()), true
	default:
		return 0, false
	}
}
