package events

import (
	"context"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// Record kinds and data keys are stored columns: renaming one is a migration, not a wording change.
const (
	keyName        = "name"
	keyCharacter   = "character" // whose sound it was, on a sound record
	kindGuildJoin  = "guild_join"
	kindGuildLeave = "guild_leave"
	kindSoundAdded = "sound_added"
	kindSoundGone  = "sound_removed"
	keyReconciled  = "reconciled"
	keyMembers     = "memberCount"
)

// Command records a slash command. It keeps its own time: the command ran when the user pressed
// enter, which may be a retry and a backlog ago.
func (l *Log) Command(ctx context.Context, guild, user snowflake.ID, name string, at time.Time) {
	l.Audit(ctx, Misc{At: at, Kind: "command", Guild: &guild, User: &user, Data: map[string]any{keyName: name}})
}

// SettingsChanged only prints. The record itself is written by the audit trigger on guild_settings,
// which sees the old and new values this call does not carry.
func (l *Log) SettingsChanged(ctx context.Context, guild snowflake.ID, field string, by snowflake.ID) {
	l.Print(ctx, Misc{Kind: "settings_changed", Guild: &guild, User: &by, Data: map[string]any{"field": field}})
}

// SoundChanged records a sound appearing or disappearing. A sound belongs to no guild.
func (l *Log) SoundChanged(ctx context.Context, character, name string, added bool) {
	kind := kindSoundGone
	if added {
		kind = kindSoundAdded
	}
	l.Audit(ctx, Misc{Kind: kind, Data: map[string]any{keyName: name, keyCharacter: character}})
}

func (l *Log) GuildJoined(ctx context.Context, guild snowflake.ID, members int) {
	l.Audit(ctx, Misc{Kind: kindGuildJoin, Guild: &guild, Data: map[string]any{keyMembers: members}})
}

// GuildLeft records a departure: live from the gateway, or reconciled at boot for one that happened
// while the bot was down. One method so the two cannot drift into writing different rows.
func (l *Log) GuildLeft(ctx context.Context, guild snowflake.ID, reconciled bool) {
	l.Audit(ctx, Misc{Kind: kindGuildLeave, Guild: &guild, Data: map[string]any{keyReconciled: reconciled}})
}
