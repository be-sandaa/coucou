package bot

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
)

// ErrShuttingDown refuses a play once the pool has begun to drain.
var ErrShuttingDown = errors.New("pool: shutting down")

// PlayConcurrency is the cap on simultaneous voice connections across the whole bot.
const PlayConcurrency = 8

// Pool runs bodies detached from their callers, at most n at a time. A full pool makes the caller
// wait, which is the backpressure; a cancelled caller is refused rather than queued.
//
// A body keeps its caller's context values but not its cancellation: /play's interaction is gone
// long before the visit ends. Nothing else stops a body, so every body needs its own deadline.
type Pool struct {
	slots   chan struct{}
	running sync.WaitGroup
	mu      sync.Mutex // holds a body's Add and Close's start apart, so nothing starts after the drain
	closed  bool
	closing chan struct{}
}

func NewPool(n int) *Pool { return &Pool{slots: make(chan struct{}, n), closing: make(chan struct{})} }

// Go starts fn once a slot is free, and returns nil as soon as it has. Once Close has begun it
// refuses with ErrShuttingDown: a play started after the drain would be cut off and never recorded.
func (p *Pool) Go(ctx context.Context, fn func(context.Context)) error {
	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closing:
		return ErrShuttingDown
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.slots
		return ErrShuttingDown
	}
	p.running.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.running.Done()
		defer func() { <-p.slots }()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("pool: body panicked", slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		fn(context.WithoutCancel(ctx))
	}()
	return nil
}

// Close waits for the running bodies, so shutdown does not cut a play off mid-frame and skip its
// cleanup. ctx bounds the wait: a body outrunning it is reported rather than waited on.
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.closing)
	}
	p.mu.Unlock()
	done := make(chan struct{})
	go func() { defer close(done); p.running.Wait() }() // ends when the last body does
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("pool: shut down with plays still running", slog.Any("err", ctx.Err()))
	}
	return nil
}
