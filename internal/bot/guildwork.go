package bot

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/be-sandaa/coucou/internal/tracing"
)

// guildQueue only fills if live joins and leaves outrun the worker; the boot burst never reaches it.
const guildQueue = 256

type guildJob struct {
	span string
	id   string
	run  func(context.Context)
}

// GuildWorker runs live guild joins and leaves one at a time, off disgo's dispatch goroutine.
// One worker keeps a guild's join and leave in order, and a join's steps in the order given.
type GuildWorker struct {
	onJoin  func(context.Context, *discord.GatewayGuild)
	onLeave func(context.Context, *discord.Guild)
	jobs    chan guildJob
	stop    chan struct{}
	running sync.WaitGroup
	mu      sync.Mutex // holds Run's Add and Close apart, so a Run after Close runs nothing
	closed  bool
}

func NewGuildWorker(join func(context.Context, *discord.GatewayGuild), leave func(context.Context, *discord.Guild)) *GuildWorker {
	return &GuildWorker{onJoin: join, onLeave: leave, jobs: make(chan guildJob, guildQueue), stop: make(chan struct{})}
}

// Listeners are the gateway half. They only enqueue, so gateway dispatch never waits on the work.
func (w *GuildWorker) Listeners() []bot.EventListener {
	return []bot.EventListener{
		bot.NewListenerFunc(func(e *guildJoined) { w.join(&e.Guild) }),
		bot.NewListenerFunc(func(e *events.GuildLeave) { w.leave(&e.Guild.Guild) }),
	}
}

func (w *GuildWorker) join(g *discord.GatewayGuild) {
	w.enqueue(guildJob{"guild join", g.ID.String(), func(ctx context.Context) { w.onJoin(ctx, g) }})
}

func (w *GuildWorker) leave(g *discord.Guild) {
	w.enqueue(guildJob{"guild leave", g.ID.String(), func(ctx context.Context) { w.onLeave(ctx, g) }})
}

// enqueue waits for room rather than dropping, and gives up once the worker is stopping.
func (w *GuildWorker) enqueue(j guildJob) {
	select {
	case w.jobs <- j:
	case <-w.stop:
	}
}

// Run works the queue until Close, not until ctx is cancelled: the runner cancels ctx at the
// signal, while the shards are still open and can still hand it a join.
func (w *GuildWorker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.running.Add(1)
	w.mu.Unlock()
	defer w.running.Done()
	for {
		select { // stop first: once both are ready, the select below would pick at random
		case <-w.stop:
			return nil
		default:
		}
		select {
		case j := <-w.jobs:
			w.do(context.WithoutCancel(ctx), j)
		case <-w.stop:
			return nil
		}
	}
}

func (w *GuildWorker) do(ctx context.Context, j guildJob) {
	ctx, span := tracing.Tracer().Start(ctx, j.span, trace.WithAttributes(attribute.String("coucou.guild", j.id)))
	defer span.End()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("guilds: job panicked", slog.String("job", j.span), slog.String("guild", j.id),
				slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
		}
	}()
	j.run(ctx)
}

// Close stops intake and waits, within ctx, for the job in hand.
func (w *GuildWorker) Close(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.stop)
	}
	w.mu.Unlock()
	done := make(chan struct{})
	go func() { defer close(done); w.running.Wait() }() // ends when Run does
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("guilds: shut down with a job still running", slog.Any("err", ctx.Err()))
	}
	return nil
}

// SyncBudget is how long a guild's database sync retries before the boot reconcile is left to fix it.
const SyncBudget = 30 * time.Second

// Retry calls fn until it succeeds, budget runs out (returning the last error) or ctx is done,
// backing off from 200ms to 5s. budget is long enough to ride out a database failover.
func Retry(ctx context.Context, budget time.Duration, fn func(context.Context) error) error {
	deadline := time.Now().Add(budget)
	// The deadline bounds each attempt too: a call that hangs would otherwise wedge the worker.
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	wait := 200 * time.Millisecond
	for {
		err := fn(ctx)
		if err == nil || time.Now().Add(wait).After(deadline) {
			return err
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		wait = min(wait*2, 5*time.Second)
	}
}
