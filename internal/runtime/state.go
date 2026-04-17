package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

type taskHandle struct {
	done       chan struct{}
	once       sync.Once
	mu         sync.RWMutex
	result     any
	err        error
	startedAt  time.Time
	finishedAt time.Time
}

func newTaskHandle() *taskHandle {
	return &taskHandle{
		done:      make(chan struct{}),
		startedAt: time.Now(),
	}
}

func (t *taskHandle) complete(result any, err error) {
	t.mu.Lock()
	t.result = cloneValue(result)
	t.err = err
	t.finishedAt = time.Now()
	t.mu.Unlock()
	t.once.Do(func() {
		close(t.done)
	})
}

func (t *taskHandle) await(ctx context.Context, timeout time.Duration) (any, bool, error) {
	if timeout < 0 {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-t.done:
		}
	} else {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			return nil, true, nil
		case <-t.done:
		}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.err != nil {
		return nil, false, t.err
	}
	return cloneValue(t.result), false, nil
}

func (t *taskHandle) snapshot() map[string]any {
	t.mu.RLock()
	defer t.mu.RUnlock()
	done := !t.finishedAt.IsZero()
	payload := map[string]any{
		"done":       done,
		"started_at": t.startedAt.UTC().Format(time.RFC3339Nano),
	}
	if done {
		payload["finished_at"] = t.finishedAt.UTC().Format(time.RFC3339Nano)
	}
	if t.err != nil {
		payload["error"] = t.err.Error()
	} else if done {
		payload["result"] = cloneValue(t.result)
	}
	return payload
}

type channelHandle struct {
	ch     chan any
	mu     sync.RWMutex
	closed bool
}

func newChannelHandle(capacity int) *channelHandle {
	return &channelHandle{ch: make(chan any, capacity)}
}

func (c *channelHandle) Send(value any, timeout time.Duration) (sent bool, err error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return false, fmt.Errorf("channel is closed")
	}
	ch := c.ch
	c.mu.RUnlock()

	defer func() {
		if recovered := recover(); recovered != nil {
			sent = false
			err = fmt.Errorf("channel is closed")
		}
	}()

	if timeout < 0 {
		ch <- cloneValue(value)
		return true, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ch <- cloneValue(value):
		return true, nil
	case <-timer.C:
		return false, nil
	}
}

func (c *channelHandle) Recv(timeout time.Duration) (value any, ok bool, timedOut bool) {
	if timeout < 0 {
		value, ok = <-c.ch
		if ok {
			return cloneValue(value), true, false
		}
		return nil, false, false
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case value, ok = <-c.ch:
		if ok {
			return cloneValue(value), true, false
		}
		return nil, false, false
	case <-timer.C:
		return nil, false, true
	}
}

func (c *channelHandle) Close() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.closed = true
	close(c.ch)
	return true
}

type mutexHandle struct {
	token chan struct{}
}

func newMutexHandle() *mutexHandle {
	token := make(chan struct{}, 1)
	token <- struct{}{}
	return &mutexHandle{token: token}
}

func (m *mutexHandle) Lock(timeout time.Duration) bool {
	if timeout < 0 {
		<-m.token
		return true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-m.token:
		return true
	case <-timer.C:
		return false
	}
}

func (m *mutexHandle) Unlock() error {
	select {
	case m.token <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("mutex is not locked")
	}
}

type safeWaitGroup struct {
	mu      sync.Mutex
	counter int
	waiting bool
	zero    chan struct{}
}

func newSafeWaitGroup() *safeWaitGroup {
	zero := make(chan struct{})
	close(zero)
	wg := &safeWaitGroup{
		zero: zero,
	}
	return wg
}

func (w *safeWaitGroup) Add(delta int) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if delta > 0 && w.waiting {
		return w.counter, fmt.Errorf("wait_group_add cannot increase the counter after wait_group_wait has started")
	}

	next := w.counter + delta
	if next < 0 {
		return w.counter, fmt.Errorf("wait group counter cannot go negative")
	}

	if w.counter == 0 && next > 0 {
		w.zero = make(chan struct{})
	}
	w.counter = next
	if w.counter == 0 {
		select {
		case <-w.zero:
		default:
			close(w.zero)
		}
	}
	return w.counter, nil
}

func (w *safeWaitGroup) Done() (int, error) {
	return w.Add(-1)
}

func (w *safeWaitGroup) Wait(timeout time.Duration) bool {
	w.mu.Lock()
	w.waiting = true
	if w.counter == 0 {
		w.mu.Unlock()
		return true
	}
	zero := w.zero
	w.mu.Unlock()

	if timeout < 0 {
		<-zero
		return true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-zero:
		return true
	case <-timer.C:
		return false
	}
}

type httpServerHandle struct {
	server   *http.Server
	listener net.Listener
	address  string
}

type socketListenerHandle struct {
	listener net.Listener
	address  string
}

func (i *Interpreter) registerFunction(function *AIFunction) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.functions[function.Name()] = function
	i.tools[function.Name()] = function
}

func (i *Interpreter) toolSpecs(exclude string, directives aiDirectiveConfig) []ToolSpec {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return sortedToolSpecs(i.tools, exclude, directives)
}

func (i *Interpreter) lookupTool(name string) (ToolCallable, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	callable, ok := i.tools[name]
	return callable, ok
}

func (i *Interpreter) nextHandle(prefix string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.nextResource++
	return fmt.Sprintf("%s_%d", prefix, i.nextResource)
}

func (i *Interpreter) storeSocket(handleID string, handle *socketHandle) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.sockets[handleID] = handle
}

func (i *Interpreter) lookupSocket(handleID string) (*socketHandle, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	handle, ok := i.sockets[handleID]
	if !ok {
		return nil, fmt.Errorf("unknown socket handle %q", handleID)
	}
	return handle, nil
}

func (i *Interpreter) closeSocket(handleID string) (*socketHandle, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	handle, ok := i.sockets[handleID]
	if !ok {
		return nil, false
	}
	delete(i.sockets, handleID)
	return handle, true
}

func (i *Interpreter) storeSocketListener(handleID string, handle *socketListenerHandle) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.socketListeners[handleID] = handle
}

func (i *Interpreter) lookupSocketListener(handleID string) (*socketListenerHandle, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	handle, ok := i.socketListeners[handleID]
	if !ok {
		return nil, fmt.Errorf("unknown socket listener handle %q", handleID)
	}
	return handle, nil
}

func (i *Interpreter) closeSocketListener(handleID string) (*socketListenerHandle, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	handle, ok := i.socketListeners[handleID]
	if !ok {
		return nil, false
	}
	delete(i.socketListeners, handleID)
	return handle, true
}

func (i *Interpreter) storeTask(handleID string, handle *taskHandle) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tasks[handleID] = handle
}

func (i *Interpreter) lookupTask(handleID string) (*taskHandle, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	handle, ok := i.tasks[handleID]
	if !ok {
		return nil, fmt.Errorf("unknown task handle %q", handleID)
	}
	return handle, nil
}

func (i *Interpreter) storeChannel(handleID string, handle *channelHandle) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.channels[handleID] = handle
}

func (i *Interpreter) lookupChannel(handleID string) (*channelHandle, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	handle, ok := i.channels[handleID]
	if !ok {
		return nil, fmt.Errorf("unknown channel handle %q", handleID)
	}
	return handle, nil
}

func (i *Interpreter) closeChannel(handleID string) (bool, error) {
	handle, err := i.lookupChannel(handleID)
	if err != nil {
		return false, err
	}
	return handle.Close(), nil
}

func (i *Interpreter) storeMutex(handleID string, handle *mutexHandle) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.mutexes[handleID] = handle
}

func (i *Interpreter) lookupMutex(handleID string) (*mutexHandle, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	handle, ok := i.mutexes[handleID]
	if !ok {
		return nil, fmt.Errorf("unknown mutex handle %q", handleID)
	}
	return handle, nil
}

func (i *Interpreter) storeWaitGroup(handleID string, handle *safeWaitGroup) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.waitGroups[handleID] = handle
}

func (i *Interpreter) lookupWaitGroup(handleID string) (*safeWaitGroup, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	handle, ok := i.waitGroups[handleID]
	if !ok {
		return nil, fmt.Errorf("unknown wait group handle %q", handleID)
	}
	return handle, nil
}

func (i *Interpreter) storeServer(handleID string, handle *httpServerHandle) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.servers[handleID] = handle
}

func (i *Interpreter) closeServer(handleID string) (*httpServerHandle, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	handle, ok := i.servers[handleID]
	if !ok {
		return nil, false
	}
	delete(i.servers, handleID)
	return handle, true
}

func (i *Interpreter) incrementMetric(name string, delta int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.metrics[name] += delta
}

func (i *Interpreter) metricsSnapshot() map[string]any {
	i.mu.RLock()
	defer i.mu.RUnlock()
	keys := make([]string, 0, len(i.metrics))
	for key := range i.metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	snapshot := make(map[string]any, len(keys))
	for _, key := range keys {
		snapshot[key] = i.metrics[key]
	}
	return snapshot
}

func waitTimeout(timeoutMS int64) time.Duration {
	if timeoutMS < 0 {
		return -1
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func (i *Interpreter) awaitTask(ctx context.Context, handleID string, timeoutMS int64) (any, bool, error) {
	handle, err := i.lookupTask(handleID)
	if err != nil {
		return nil, false, err
	}
	return handle.await(ctx, waitTimeout(timeoutMS))
}
