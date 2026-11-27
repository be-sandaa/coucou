// Package app is the composition root: it turns arguments into a configuration, builds everything
// the bot needs in dependency order, and runs it until a signal or the first failure.
package app

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync/atomic"
	"time"

	dbot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/be-sandaa/coucou/internal/bot"
	"github.com/be-sandaa/coucou/internal/chaos"
	"github.com/be-sandaa/coucou/internal/characters"
	"github.com/be-sandaa/coucou/internal/commands"
	ev "github.com/be-sandaa/coucou/internal/events"
	"github.com/be-sandaa/coucou/internal/logging"
	"github.com/be-sandaa/coucou/internal/metrics"
	"github.com/be-sandaa/coucou/internal/ops"
	"github.com/be-sandaa/coucou/internal/profile"
	"github.com/be-sandaa/coucou/internal/ranks"
	"github.com/be-sandaa/coucou/internal/rollup"
	"github.com/be-sandaa/coucou/internal/settings"
	"github.com/be-sandaa/coucou/internal/silence"
	"github.com/be-sandaa/coucou/internal/store"
	"github.com/be-sandaa/coucou/internal/tracing"
	"github.com/be-sandaa/coucou/internal/voice"
	"github.com/be-sandaa/coucou/pkg/run"
)

// ErrUsage marks a bad invocation rather than a failure at runtime, so the caller can exit 2 the
// way a command is expected to for a usage error.
var ErrUsage = errors.New("usage")

// maxAckAge is four missed 41s heartbeats: a zombie shard reconnects well inside it, a wedged
// dispatch never does. See bot.Pulse.
const maxAckAge = 3 * time.Minute

// Run is the whole program. It returns an error instead of exiting so every deferred close still
// runs — the database in particular, which the final stats flush writes to.
//
// build is stamped into the binary at link time; it reaches the banner and the trace resource.
func Run(args []string, build Build) error {
	// A bootstrap logger at Info, because -log-level has not been read yet and a configuration
	// error still has to be printable.
	slog.SetDefault(slog.New(logging.New(os.Stdout, slog.LevelInfo)))

	cfg, err := parseConfig(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err // -h already printed the usage
		}
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	// Now that -log-level has been read, replace the bootstrap logger. This is the last time it is
	// set: audit lines are ordinary Info records, so nothing downstream needs its own handler.
	slog.SetDefault(slog.New(logging.New(os.Stdout, cfg.LogLevel)))

	// The characters are read before anything opens: a bot with a broken profile is misconfigured,
	// not degraded, and should say so before it touches the database or Discord.
	chars, err := loadCharacters(cfg)
	if err != nil {
		return fmt.Errorf("%w: profile: %w", ErrUsage, err)
	}

	return serve(cfg, chars, build)
}

// loadCharacters reads the characters the bot starts with.
func loadCharacters(cfg config) (*characters.Set, error) {
	ps, err := loadProfiles(cfg)
	if err != nil {
		return nil, err
	}
	return characters.New(ps, cfg.DefaultProfile)
}

// loadProfiles reads profiles/, or the one deprecated profile directory.
func loadProfiles(cfg config) ([]profile.Profile, error) {
	if cfg.ProfilesDir != "" {
		return profile.LoadAll(cfg.ProfilesDir)
	}
	p, err := profile.Load(cfg.ProfileDir)
	if err != nil {
		return nil, err
	}
	return []profile.Profile{p}, nil
}

// friends is the siblings from config plus every character with a bot of its own, once per app.
// The config's entry wins a tie: it is the name the operator chose to advertise.
func friends(siblings []commands.Sibling, chars []*characters.Character) []commands.Sibling {
	out := slices.Clone(siblings)
	for _, c := range chars {
		if c.App != 0 && !slices.ContainsFunc(out, func(s commands.Sibling) bool { return s.App == c.App }) {
			out = append(out, commands.Sibling{Name: c.Name(), App: c.App})
		}
	}
	return out
}

// serve is the long-running path: open the database, build everything on top of it, and block
// until a signal or the first failure. The two modes that exit early — migrating and registering
// slash commands — return from inside it, because each needs part of what it builds.
func serve(cfg config, chars *characters.Set, build Build) error {
	r := run.New()
	defer r.Stop()

	// Database first, always. A failed migration fails the process before we ever touch Discord.
	// The backend is chosen by the URL scheme; both then wait for readiness and migrate the same way.
	db, err := store.Open(r, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db: open: %w", err)
	}
	defer db.Close()
	if err := store.WaitAndMigrate(r, db); err != nil {
		return fmt.Errorf("db: refusing to start: %w", err)
	}

	p, err := assemble(r, cfg, chars, db)
	if err != nil {
		return err
	}
	// Commands are registered on the way up rather than by a separate one-off run, so a build can
	// never be serving a command set it does not have the handlers for. It is a REST call; the
	// gateway is not open yet.
	if err := commands.Deploy(p.client, p.commands.Definitions()); err != nil {
		return err
	}

	ops.Serve(r, cfg.HTTPAddr, cfg.PProf, func() error {
		if age := p.pulse.Age(); age > maxAckAge {
			return fmt.Errorf("no gateway heartbeat ACK for %s", age.Round(time.Second))
		}
		return nil
	}, func(ctx context.Context) error {
		if !bot.Ready(p.client) {
			return errors.New("discord gateway is not ready")
		}
		return db.Ping(ctx)
	})

	// Registered before the bot, so the exporter is shut down — and its batch
	// flushed — only once nothing is still producing spans.
	stopTracing, err := tracing.Init(r, cfg.OTLPEndpoint, build.Version)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	r.Add(nil, stopTracing)

	// Registration order is shutdown order reversed: the stats flusher is registered first so it is
	// stopped last and still has a live database to write its final batch to.
	r.Add(run.Ctx(p.events.Run), run.Ctx(p.events.Flush))
	// From here a sound change is recorded; the first scan, in assemble, only printed.
	r.Add(func(context.Context) error { p.live.Store(true); return nil }, nil)
	// After the log, so plays drain before its last flush and a play cut off by shutdown is recorded.
	r.Add(nil, p.pool.Close)
	// After the log too, so the job in hand at shutdown still lands in its last flush.
	r.Add(p.guilds.Run, p.guilds.Close)
	// Open, then reconcile, in that order and in one func. OpenShardManager returns once every
	// shard has had its READY, which makes the line after it the first moment the cache holds a
	// complete picture of what the bot is in — and a complete picture is exactly what the
	// reconcile compares the database against.
	r.Add(func(ctx context.Context) error {
		if err := p.client.OpenShardManager(ctx); err != nil {
			return err
		}
		if err := bot.SyncGuilds(ctx, p.client, p.ready, p.events, db, p.settings, chars.Default().Defaults); err != nil {
			return err
		}
		printBanner(os.Stdout, build, p.client, chars.Len())
		// After the reconcile, so every present guild has its settings row to read the character from.
		p.persona.PushAll()
		return nil
	}, func(ctx context.Context) error {
		// Plays first: Close would otherwise yank their voice conns mid-frame, under their own close.
		voice.Shutdown(ctx)
		p.client.Close(ctx)
		return nil
	})
	// Recurring opt-outs, quiet and chaos are windows the loop reads from memory; without these tickers
	// they would stay frozen at whichever occurrence was live when the process started.
	r.Add(p.optouts.Run, nil)
	r.Add(p.quiet.Run, nil)
	r.Add(p.chaos.Run, nil)
	r.Add(p.ranks.Run, nil)
	r.Add(p.commands.RunEmojis, nil)
	r.Add(p.rollup.Run, nil)
	r.Add(p.loop.Run, nil)
	r.Add(p.persona.Run, nil)
	r.Add(watchCharacters(cfg, chars, p), nil)

	slog.Info("running")
	return r.Run()
}

// assemble builds everything between the database and Discord — settings, the sound registry, the
// stats log, the play pool and the guild worker — then the bot that uses them.
// parts is what serve registers, named rather than returned positionally — five values in a row
// is a signature nobody reads correctly twice.
type parts struct {
	client   *dbot.Client
	ready    *bot.ReadyTracker
	pulse    *bot.Pulse
	events   *ev.Log
	pool     *bot.Pool
	guilds   *bot.GuildWorker
	live     *atomic.Bool
	loop     *bot.Loop
	optouts  *silence.Store
	quiet    *silence.Store
	chaos    *chaos.Store
	ranks    *ranks.Cuts
	rollup   *rollup.Refresher
	settings *settings.Store
	commands *commands.Commands
	persona  *bot.Persona
}

// mem is the tables the bot answers from memory.
type mem struct {
	settings *settings.Store
	optouts  *silence.Store
	quiet    *silence.Store
	chaos    *chaos.Store
}

func mirrors(ctx context.Context, db store.Store) (mem, error) {
	m := mem{settings: settings.New(db), optouts: silence.OptOuts(db), quiet: silence.Quiet(db), chaos: chaos.New(db)}
	for name, load := range map[string]func(context.Context) error{
		"settings": m.settings.Load, "optouts": m.optouts.Load, "quiet": m.quiet.Load, "chaos": m.chaos.Load,
	} {
		if err := load(ctx); err != nil {
			return mem{}, fmt.Errorf("%s: load: %w", name, err)
		}
	}
	return m, nil
}

func assemble(r *run.Runner, cfg config, chars *characters.Set, db store.Store) (*parts, error) {
	m, err := mirrors(r, db)
	if err != nil {
		return nil, err
	}
	set, opt, quiet, cha := m.settings, m.optouts, m.quiet, m.chaos

	log := ev.New(db)
	live := &atomic.Bool{}
	if err := chars.Start(r, cfg.SoundsPoll, func(character string) func(string, bool) {
		return soundRecorder(r, log, live, character)
	}); err != nil {
		return nil, fmt.Errorf("sounds: start: %w", err)
	}

	// Status is the whole account's, so with several characters it is the default's.
	client, err := bot.New(cfg.Token, cfg.ShardCount, botStatus(cfg, chars))
	if err != nil {
		return nil, err
	}
	warnForeignApp(chars, client.ApplicationID)
	// The player is built before the two things that use it — the loop and /play — because it is
	// the shared cap on simultaneous voice connections, not a per-caller one.
	pool := bot.NewPool(bot.PlayConcurrency)
	player := bot.NewPlayer(client, chars, set, quiet, opt, log, pool)
	// /play hands off rather than calling straight through: a full pool makes the caller wait, and
	// the caller here is a gateway handler.
	//
	// WithoutCancel, not Background: the command's span is the trace root a play hangs off.
	// The cancellation has to go — the interaction's context dies when the handler returns, and a
	// play outlives it by ninety seconds or more.
	play := func(ctx context.Context, a commands.PlayArgs) {
		ctx = context.WithoutCancel(ctx)
		go func() {
			if err := player(ctx, &bot.PlayRequest{
				Guild: a.Guild, Channel: a.Channel, Sound: a.Sound, Trigger: string(ev.TriggerCommand), User: &a.User,
				Character: a.Character, Preview: a.Preview,
			}); err != nil {
				slog.Debug("play: refused", slog.Any("err", err))
			}
		}()
	}
	rk := ranks.New(db)
	persona := bot.NewPersona(client, chars, set)
	cmds := commands.New(client, set, opt, quiet, cha, chars, log, rk, play, cfg.OwnerIDs, friends(cfg.Siblings, chars.All()), cfg.Note, cfg.Color, persona.Push)
	ready := bot.NewReadyTracker()
	pulse := bot.NewPulse()

	// Everything coming in from Discord. Each listener is built with what it acts on and nothing
	// more, which is why two of them take the client and the rest do not.
	// The guild reconcile is not here. It needs every shard to have reported, which no gateway
	// event says — serve sequences it after OpenShardManager instead. See bot.SyncGuilds.
	// Live guild joins and leaves go to one worker, off the gateway's dispatch goroutine: a join
	// writes to the database with retries, and a wedged dispatch stalls every shard. Interactions
	// stay synchronous — they have three seconds to answer.
	guilds := guildWorker(log, db, set, chars.Default().Defaults, persona, cmds)
	client.AddEventListeners(slices.Concat(guilds.Listeners(), cmds.Listeners(),
		[]dbot.EventListener{ready.OnReady(), pulse.OnHeartbeatAck()})...)

	metrics.ObserveGuilds(func() int { return len(set.Configured()) })
	metrics.ObserveSounds(chars.Len)
	metrics.ObserveBuffered(log.Buffered)
	metrics.ObserveGatewayLatency(bot.GatewayLatency(client))

	return &parts{
		client:   client,
		ready:    ready,
		pulse:    pulse,
		events:   log,
		guilds:   guilds,
		pool:     pool,
		live:     live,
		loop:     bot.NewLoop(client, set, chars, player, opt, quiet, cha),
		optouts:  opt,
		quiet:    quiet,
		chaos:    cha,
		ranks:    rk,
		rollup:   rollup.New(db),
		settings: set,
		commands: cmds,
		persona:  persona,
	}, nil
}

// watchCharacters reloads the characters when their profiles change. A reload can add or remove
// /character and change any server's nickname or avatar. The embed accent and the status are read
// once, so those two still wait for a restart.
func watchCharacters(cfg config, chars *characters.Set, p *parts) func(context.Context) error {
	return func(ctx context.Context) error {
		return chars.Watch(ctx, cmp.Or(cfg.ProfilesDir, cfg.ProfileDir), func() ([]profile.Profile, error) {
			return loadProfiles(cfg)
		}, func() {
			if err := commands.Deploy(p.client, p.commands.Definitions()); err != nil {
				slog.Error("characters: redeploy commands", slog.Any("err", err))
			}
			p.persona.PushAll()
		})
	}
}

// botStatus is the bot's status: config.toml's, else the default character's from before it moved there.
func botStatus(cfg config, chars *characters.Set) profile.Status {
	if cfg.Status != nil {
		return *cfg.Status
	}
	return chars.Default().Status
}

// warnForeignApp catches a single character whose application_id is some other bot's, which is a
// profile copied from another deployment rather than anything this bot can act on.
func warnForeignApp(chars *characters.Set, self snowflake.ID) {
	if all := chars.All(); len(all) == 1 && all[0].App != 0 && all[0].App != self {
		slog.Warn("profile: application_id is another bot's; was this profile copied from another deployment?",
			slog.String("profile", all[0].ID), slog.String("application_id", all[0].App.String()))
	}
}

// soundRecorder records registry changes once the bot is live. The first scan at boot reports every
// sound as added; recording those would store a sound_added per sound on every restart, so until
// live they are only printed.
func soundRecorder(ctx context.Context, log *ev.Log, live *atomic.Bool, character string) func(name string, added bool) {
	return func(name string, added bool) {
		if !live.Load() {
			slog.Info("sound changed before the bot was up", slog.String("character", character), slog.String("name", name), slog.Bool("added", added))
			return
		}
		log.SoundChanged(ctx, character, name, added)
	}
}

// guildWorker is what a live join and leave cause, in order. Only the database sync retries: the
// boot reconcile repairs a write that gave up, and a retried welcome could post twice.
func guildWorker(log *ev.Log, db store.Store, set *settings.Store, d store.Defaults, persona *bot.Persona, cmds *commands.Commands) *bot.GuildWorker {
	joinSync, leaveSync := bot.GuildJoinedSync(db, set, d), bot.GuildLeftSync(db)
	return bot.NewGuildWorker(
		func(ctx context.Context, g *discord.GatewayGuild) {
			log.GuildJoined(ctx, g.ID, g.MemberCount)
			if err := bot.Retry(ctx, bot.SyncBudget, func(ctx context.Context) error { return joinSync(ctx, g) }); err != nil {
				slog.Error("guilds: join sync gave up", slog.String("guild", g.ID.String()), slog.Any("err", err))
			}
			// After the sync: the persona reads the settings row it seeds.
			persona.Push(g.ID)
			if err := cmds.Welcome(ctx, g); err != nil {
				slog.Warn("guilds: welcome", slog.String("guild", g.ID.String()), slog.Any("err", err))
			}
		},
		func(ctx context.Context, g *discord.Guild) {
			log.GuildLeft(ctx, g.ID, false)
			if err := bot.Retry(ctx, bot.SyncBudget, func(ctx context.Context) error { return leaveSync(ctx, g) }); err != nil {
				slog.Error("guilds: leave sync gave up", slog.String("guild", g.ID.String()), slog.Any("err", err))
			}
		},
	)
}
