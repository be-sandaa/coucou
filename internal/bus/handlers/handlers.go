// Package handlers holds every consumer on the event bus. Nothing here talks to Discord: the bot
// publishes what happened, this package decides what that causes.
//
// Playing is deliberately not here. A play is something the bot does on request, not something a
// delivery causes — and no ack can honestly cover the 90 seconds one can take. The loop and /play
// call bot.NewPlayer directly; what reaches this package is PlayFinished, the fact that it ended.
//
//	discord.guild_create ─► guild sync (db) ─► stats writer ─► persona queue ─► welcome (commands.Welcome)
//	discord.guild_delete ─► guild sync (db) ─► stats writer
//	PlayFinished   ─► stats writer
//	CommandInvoked ─► stats writer
//	GuildLeft      ─► stats writer (boot reconcile only)
//	SoundAdded/Removed             ─► (nobody yet; log only)
//	SettingsChanged                ─► console line only (the record is the settings trigger, or the quiet row)
//
// Each handler is built by its own constructor taking exactly what it acts on, and the result goes
// to bus.On at the composition root. Names match the event they consume; the two events that have
// two consumers each get the consumer's job as a suffix.
package handlers

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/be-sandaa/coucou/internal/bus"
	"github.com/be-sandaa/coucou/internal/events"
	"github.com/be-sandaa/coucou/internal/settings"
	"github.com/be-sandaa/coucou/internal/store"
)

func GuildJoinedStats(log *events.Log) bus.Handler[discord.GatewayGuild] {
	return func(ctx context.Context, g *discord.GatewayGuild) error {
		log.GuildJoined(ctx, g.ID, g.MemberCount)
		return nil
	}
}

func GuildLeftStats(log *events.Log) bus.Handler[discord.Guild] {
	return func(ctx context.Context, g *discord.Guild) error {
		log.GuildLeft(ctx, g.ID, false)
		return nil
	}
}

// GuildJoinedSync keeps the guilds table and the settings seed in step with Discord, and fills in a
// zone guessed from the guild's locale if it has none.
// d seeds a guild that has no settings row yet; a guild overrides each value with its command, and
// seeding never overwrites a row that already exists.
func GuildJoinedSync(db store.Store, set *settings.Store, d store.Defaults) bus.Handler[discord.GatewayGuild] {
	return func(ctx context.Context, g *discord.GatewayGuild) error {
		if err := db.UpsertGuilds(ctx, []store.Guild{{ID: g.ID, JoinedAt: g.JoinedAt}}); err != nil {
			return err // retried by middleware, then dropped with a log line
		}
		if err := db.SeedSettingsFor(ctx, g.ID, d); err != nil {
			return err
		}
		// A guild coming back already has its row, and the mirror with it: writing the defaults over
		// it here would undo the seed's do-not-overwrite rule.
		if d != (store.Defaults{}) && !set.Known(g.ID) {
			// Actor zero: nobody asked for this, the bot is applying its own defaults.
			if _, err := set.Update(ctx, g.ID, 0, func(s *settings.Settings) {
				s.Chance, s.Suspense, s.FakeOut, s.Encore = d.Chance, d.Suspense, d.FakeOut, d.Encore
			}); err != nil {
				return err
			}
		}
		// The join payload carries the locale, so the zone is filled in now rather than at the next
		// boot. A guild coming back keeps the zone it had.
		_, err := set.FillZones(ctx, map[snowflake.ID]discord.Locale{g.ID: discord.Locale(g.PreferredLocale)})
		return err
	}
}

// GuildJoinedPersona queues a joined guild to take on its character's nickname and avatar. It only
// queues: the persona's pace gives GuildJoinedSync time to seed the settings row it reads.
func GuildJoinedPersona(push func(...snowflake.ID)) bus.Handler[discord.GatewayGuild] {
	return func(_ context.Context, g *discord.GatewayGuild) error {
		push(g.ID)
		return nil
	}
}

// GuildLeftSync has no reconciled branch any more: a boot reconcile writes its own rows and
// publishes bus.GuildLeft, which this does not consume. What reaches here left while we watched.
func GuildLeftSync(db store.Store) bus.Handler[discord.Guild] {
	return func(ctx context.Context, g *discord.Guild) error {
		return db.MarkGuildLeft(ctx, g.ID)
	}
}
