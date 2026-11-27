package bot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolCapsConcurrency(t *testing.T) {
	p := NewPool(2)
	var now, peak atomic.Int32
	release := make(chan struct{})
	var started sync.WaitGroup
	for range 5 {
		started.Add(1)
		go func() {
			if err := p.Go(context.Background(), func(context.Context) {
				n := now.Add(1)
				for {
					m := peak.Load()
					if n <= m || peak.CompareAndSwap(m, n) {
						break
					}
				}
				started.Done()
				<-release
				now.Add(-1)
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	if n := now.Load(); n != 2 {
		t.Errorf("in flight = %d, want 2", n)
	}
	close(release)
	started.Wait()
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m := peak.Load(); m != 2 {
		t.Errorf("peak = %d, want 2", m)
	}
}

// A play outlives the request that started it: /play's interaction dies long before the visit ends.
func TestPoolBodySurvivesItsCaller(t *testing.T) {
	p := NewPool(1)
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan error, 1)
	if err := p.Go(ctx, func(ctx context.Context) {
		time.Sleep(20 * time.Millisecond)
		got <- ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-got; err != nil {
		t.Errorf("body ctx = %v, want still live", err)
	}
}

func TestPoolRefusesWhenCancelled(t *testing.T) {
	p := NewPool(1)
	hold := make(chan struct{})
	defer close(hold)
	if err := p.Go(context.Background(), func(context.Context) { <-hold }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Go(ctx, func(context.Context) { t.Error("ran on a full, cancelled pool") }); !errors.Is(err, context.Canceled) {
		t.Errorf("Go = %v, want context.Canceled", err)
	}
}

func TestPoolCloseWaits(t *testing.T) {
	p := NewPool(1)
	var done atomic.Bool
	if err := p.Go(context.Background(), func(context.Context) {
		time.Sleep(30 * time.Millisecond)
		done.Store(true)
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !done.Load() {
		t.Error("Close returned before the body finished")
	}
}

func TestPoolRecoversAPanic(t *testing.T) {
	p := NewPool(1)
	if err := p.Go(context.Background(), func(context.Context) { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{})
	if err := p.Go(context.Background(), func(context.Context) { close(ran) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("the slot a panicking body held was never freed")
	}
}
