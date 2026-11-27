package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// Each record's kind and data keys are schema: these pin the rows the bus handlers used to write.
func TestRecords(t *testing.T) {
	const bart, knock = "bart", "knock"
	g, u := snowflake.ID(11), snowflake.ID(22)
	then := time.Now().Add(-time.Hour).Truncate(time.Second)
	tests := []struct {
		name   string
		record func(*Log)
		kind   string
		guild  *snowflake.ID
		user   *snowflake.ID
		data   map[string]any
		at     time.Time
	}{
		{"command", func(l *Log) { l.Command(context.Background(), g, u, soundPlay, then) },
			kindCommand, &g, &u, map[string]any{keyName: soundPlay}, then},
		{"sound added", func(l *Log) { l.SoundChanged(context.Background(), bart, knock, true) },
			kindSoundAdded, nil, nil, map[string]any{keyName: knock, keyCharacter: bart}, time.Time{}},
		{"sound removed", func(l *Log) { l.SoundChanged(context.Background(), bart, knock, false) },
			kindSoundGone, nil, nil, map[string]any{keyName: knock, keyCharacter: bart}, time.Time{}},
		{"guild joined", func(l *Log) { l.GuildJoined(context.Background(), g, 42) },
			kindGuildJoin, &g, nil, map[string]any{keyMembers: float64(42)}, time.Time{}},
		{"guild left", func(l *Log) { l.GuildLeft(context.Background(), g, false) },
			kindGuildLeave, &g, nil, map[string]any{keyReconciled: false}, time.Time{}},
		{"guild reconciled", func(l *Log) { l.GuildLeft(context.Background(), g, true) },
			kindGuildLeave, &g, nil, map[string]any{keyReconciled: true}, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &capture{}
			l := New(c)
			_ = withConsole(t, slog.LevelInfo)
			tt.record(l)
			l.Flush(context.Background())
			rows := c.rows()
			if len(rows) != 1 {
				t.Fatalf("got %d rows, want 1", len(rows))
			}
			r := rows[0]
			if r.Kind != tt.kind || !sameID(r.Guild, tt.guild) || !sameID(r.User, tt.user) {
				t.Errorf("row = kind %q guild %v user %v, want %q %v %v", r.Kind, r.Guild, r.User, tt.kind, tt.guild, tt.user)
			}
			var data map[string]any
			if err := json.Unmarshal(r.Data, &data); err != nil || !maps.Equal(data, tt.data) {
				t.Errorf("data = %v (%v), want %v", data, err, tt.data)
			}
			if !tt.at.IsZero() && !r.At.Equal(tt.at) {
				t.Errorf("at = %v, want %v", r.At, tt.at)
			}
		})
	}
}

// A settings change is only printed: the settings trigger writes the record.
func TestSettingsChangedOnlyPrints(t *testing.T) {
	c := &capture{}
	l := New(c)
	buf := withConsole(t, slog.LevelInfo)
	l.SettingsChanged(context.Background(), 11, "chance", 22)
	l.Flush(context.Background())
	if rows := c.rows(); len(rows) != 0 {
		t.Errorf("got %d rows, want none", len(rows))
	}
	if out := buf.String(); !strings.Contains(out, "settings_changed") || !strings.Contains(out, "field=chance") {
		t.Errorf("console = %q, want a settings_changed line naming the field", out)
	}
}

func sameID(a, b *snowflake.ID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
