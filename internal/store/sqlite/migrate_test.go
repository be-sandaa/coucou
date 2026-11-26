package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/be-sandaa/coucou/internal/store"
)

// 00013 rebuilds stats_plays to widen its trigger check. Dropping the old table with foreign keys on
// would cascade into stats_play_listeners, so the rows written before it must still be there after,
// and the cascade must still work once the swap is done.
func TestEncoreRebuildKeepsListeners(t *testing.T) {
	ctx := context.Background()
	s := migratedTo(t, 12)
	exec(t, s, `insert into stats_plays (id, at, guild_id, channel_id, sound, trigger, listeners, ok, duration_ms) values (1, '2026-01-01T00:00:00Z', 1, 2, 'a', 'loop', 1, 1, 10)`)
	exec(t, s, `insert into stats_play_listeners (play_id, user_id) values (1, 3)`)
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := listeners(t, s); n != 1 {
		t.Fatalf("%d listener rows after the rebuild, want 1", n)
	}
	exec(t, s, `insert into stats_plays (at, guild_id, channel_id, sound, trigger, listeners, ok, duration_ms) values ('2026-01-01T00:00:00Z', 1, 2, 'b', 'encore', 0, 1, 10)`)
	exec(t, s, `delete from stats_plays where id = 1`)
	if n := listeners(t, s); n != 0 {
		t.Errorf("%d listener rows after deleting their play, want 0: the cascade did not survive", n)
	}
}

// 00021 gives every play recorded before it its one sound, so heard and top sounds keep their history.
func TestPlaySoundsBackfill(t *testing.T) {
	s := migratedTo(t, 20)
	exec(t, s, `insert into stats_plays (id, at, guild_id, channel_id, sound, trigger, listeners, ok, duration_ms) values (1, '2026-01-01T00:00:00Z', 1, 2, 'a', 'loop', 1, 1, 10), (2, '2026-01-01T00:00:00Z', 1, 2, 'b', 'loop', 1, 0, 10)`)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if got := playSounds(t, s); got != "1:0:a:0:1 2:0:b:0:0" {
		t.Errorf("play sounds = %q, want each play's own sound at 0, played as it went", got)
	}
}

// A visit's clips and its suspense are stored as planned, in order, played or not: enough to replay it.
func TestPlaySoundsAreStored(t *testing.T) {
	const knock, loop = "knock", "loop"
	ctx := context.Background()
	s := migratedTo(t, 21)
	if err := s.WritePlays(ctx, []store.Play{{At: time.Now().UTC(), Guild: 1, Channel: 2, Sound: knock, Trigger: loop,
		OK: true, Duration: time.Second, Suspense: 3 * time.Second,
		Sounds: []store.PlaySound{{Sound: knock, Played: true}, {Sound: "who", Gap: 2500 * time.Millisecond, Played: true}, {Sound: "rim"}},
	}, {At: time.Now().UTC(), Guild: 1, Channel: 2, Sound: "old", Trigger: loop, OK: true, Duration: time.Second}}); err != nil {
		t.Fatalf("WritePlays: %v", err)
	}
	if got := playSounds(t, s); got != "1:0:knock:0:1 1:1:who:2500:1 1:2:rim:0:0 2:0:old:0:1" {
		t.Errorf("play sounds = %q", got)
	}
	var suspense int
	if err := s.db.QueryRowContext(ctx, `select suspense_ms from stats_plays where id = 1`).Scan(&suspense); err != nil || suspense != 3000 {
		t.Errorf("suspense_ms = %d, %v; want 3000", suspense, err)
	}
}

func playSounds(t *testing.T, s *Store) string {
	t.Helper()
	rows, err := s.db.QueryContext(context.Background(), `select play_id, position, sound, gap_ms, played from stats_play_sounds order by play_id, position`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []string
	for rows.Next() {
		var id, pos, gap, played int
		var sound string
		if err := rows.Scan(&id, &pos, &sound, &gap, &played); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, fmt.Sprintf("%d:%d:%s:%d:%d", id, pos, sound, gap, played))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return strings.Join(out, " ")
}

func migratedTo(t *testing.T, version int64) *Store {
	t.Helper()
	st, err := Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(st.Close)
	s, ok := st.(*Store)
	if !ok {
		t.Fatalf("open returned %T, want *Store", st)
	}
	goose.SetBaseFS(migrations)
	goose.SetTableName(gooseTable)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(context.Background(), s.db, "migrations", version); err != nil {
		t.Fatalf("migrate to %d: %v", version, err)
	}
	return s
}

func exec(t *testing.T, s *Store, q string) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func listeners(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), `select count(*) from stats_play_listeners`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
