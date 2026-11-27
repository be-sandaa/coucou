package bot

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

// calls is what a worker's join and leave funcs saw, in order.
type calls struct {
	mu  sync.Mutex
	got []string
}

func (c *calls) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, s)
}

func (c *calls) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.got)
}

func worker(t *testing.T, c *calls, hold <-chan struct{}) *GuildWorker {
	t.Helper()
	w := NewGuildWorker(
		func(_ context.Context, g *discord.GatewayGuild) {
			if hold != nil {
				<-hold
			}
			c.add("join " + g.ID.String())
		},
		func(_ context.Context, g *discord.Guild) { c.add("leave " + g.ID.String()) },
	)
	go func() {
		if err := w.Run(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	return w
}

func waitFor(t *testing.T, c *calls, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(c.all()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("got %v, want %d calls", c.all(), n)
		}
		time.Sleep(time.Millisecond)
	}
	return c.all()
}

func TestGuildWorkerKeepsOrder(t *testing.T) {
	c := &calls{}
	w := worker(t, c, nil)
	w.join(gatewayGuild(1, time.Time{}))
	w.leave(&discord.Guild{ID: 1})
	w.join(gatewayGuild(2, time.Time{}))
	if got, want := waitFor(t, c, 3), []string{"join 1", "leave 1", "join 2"}; !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// A full queue makes the gateway listener wait: a join is never dropped.
func TestGuildWorkerFullQueueWaits(t *testing.T) {
	c := &calls{}
	hold := make(chan struct{})
	w := worker(t, c, hold)
	n := guildQueue + 5
	sent := make(chan struct{})
	go func() {
		for i := range n {
			w.join(gatewayGuild(snowflake.ID(i+1), time.Time{}))
		}
		close(sent)
	}()
	select {
	case <-sent:
		t.Fatal("every join was taken while the worker was stuck: the queue is not bounded")
	case <-time.After(50 * time.Millisecond):
	}
	close(hold)
	<-sent
	if got := waitFor(t, c, n); len(got) != n {
		t.Errorf("ran %d joins, want %d", len(got), n)
	}
}

func TestGuildWorkerEnqueueAfterCloseReturns(t *testing.T) {
	w := NewGuildWorker(func(context.Context, *discord.GatewayGuild) {}, func(context.Context, *discord.Guild) {})
	if err := w.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for range guildQueue + 1 {
			w.join(&discord.GatewayGuild{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueue after Close blocked the gateway")
	}
}

func TestGuildWorkerRecoversAPanic(t *testing.T) {
	c := &calls{}
	w := NewGuildWorker(
		func(context.Context, *discord.GatewayGuild) { panic("boom") },
		func(_ context.Context, g *discord.Guild) { c.add("leave " + g.ID.String()) },
	)
	go func() {
		if err := w.Run(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	w.join(&discord.GatewayGuild{})
	w.leave(&discord.Guild{ID: 7})
	if got := waitFor(t, c, 1); got[0] != "leave 7" {
		t.Errorf("calls = %v, want the leave after the panic", got)
	}
}

var errFlaky = errors.New("flaky")

func TestRetryUntilSuccess(t *testing.T) {
	n := 0
	err := Retry(t.Context(), time.Second, func(context.Context) error {
		n++
		if n < 3 {
			return errFlaky
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Errorf("Retry = %v after %d calls, want nil after 3", err, n)
	}
}

func TestRetryGivesUpAtBudget(t *testing.T) {
	start := time.Now()
	err := Retry(t.Context(), 300*time.Millisecond, func(context.Context) error { return errFlaky })
	if !errors.Is(err, errFlaky) {
		t.Errorf("Retry = %v, want the last error", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("gave up after %v, want about the budget", d)
	}
}

// A call that never returns on its own (a half-open connection) is cut off at the budget, so one
// hung write cannot wedge the only guild worker.
func TestRetryBoundsAHungCall(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- Retry(t.Context(), 100*time.Millisecond, func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Retry = %v, want the deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Retry waited on a hung call past its budget")
	}
}

func TestRetryStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	n := 0
	err := Retry(ctx, time.Minute, func(context.Context) error {
		n++
		cancel()
		return errFlaky
	})
	if n != 1 || err == nil {
		t.Errorf("Retry = %v after %d calls, want an error after 1", err, n)
	}
}
