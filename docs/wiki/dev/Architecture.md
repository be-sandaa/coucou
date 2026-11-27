# Architecture

How coucou is put together, for hacking on it. Running a bot needs none of this.

## How a play happens

The loop decides *what*; the play pool decides *when it can*. The player returns as soon as the
visit has one of eight slots, and the visit runs detached — which is why shutdown drains the pool
before the stats log writes its last batch.

```mermaid
sequenceDiagram
    autonumber
    participant Sched as five-minute loop
    participant Cache as disgo cache
    participant Worker as play pool
    participant Log as stats log
    participant Discord

    Sched->>Sched: roll the dice, check quiet hours and busy
    Sched->>Cache: busiest usable channel (no REST)
    Cache-->>Sched: channel + headcount
    Sched->>Worker: play here
    Note over Sched,Worker: the call takes one of 8 slots and returns —<br/>the play itself runs detached
    Worker->>Discord: join
    Discord-->>Worker: DAVE handshake complete
    Worker->>Worker: suspense, then re-check the room is populated
    Worker->>Worker: wait out the join chime (1 s, less any suspense)
    Worker->>Discord: the clip, or a chain's clips, then leave
    Worker->>Log: RecordPlay
```

Choosing where to drop in is cache-only and spread over three places — the dice and the clock in
`internal/bot/loop.go`, the channel rules in `voice.Usable`, the policy in `voice.Best`:

```mermaid
flowchart TD
    T[five-minute tick] --> C{chance roll won?}
    C -- no --> S[skip this guild]
    C -- yes --> Q{inside quiet hours?}
    Q -- yes --> S
    Q -- no --> P{already playing here?}
    P -- yes --> S
    P -- no --> E[for each cached channel]

    E --> A{the guild's AFK channel?}
    A -- yes --> N[not a candidate]
    A -- no --> U{"voice channel? (never a stage)"}
    U -- no --> N
    U -- yes --> H{anyone in it?}
    H -- no --> N
    H -- yes --> M{"bot has View + Connect + Speak?"}
    M -- no --> N
    M -- yes --> O[subtract people who ran /optout on]
    O --> W[busiest channel left wins]
```

Two of those are deliberate policy rather than capability, and both are in `Best` rather than
`Usable` so that `/play` and `/status` are unaffected: the AFK channel is skipped because a full
one means nobody is listening, and opted-out people are subtracted from the headcount but not
hidden — a room where somebody else is present is still fair game.

## Layout

```
cmd/coucou               one func: run the app, turn its error into an exit code
internal/app             composition root: TOML config, boot order, who a guild join reaches
internal/ops             /healthz, /readyz and /metrics
internal/store           Store interface + DATABASE_URL scheme registry + WaitAndMigrate
internal/store/pg        pgx/v5, goose under pg_advisory_lock, sqlc postgresql engine
internal/store/sqlite    modernc.org/sqlite (pure Go, WAL), goose sqlite3, sqlc sqlite engine
internal/profile         the character: profile.toml, its defaults and limits, and where its sounds are
internal/settings        per-guild chance / suspense / zone, in-memory over the store
internal/silence         per-user opt-outs and per-guild quiet: always or on an RFC 5545 rule, either with an end
internal/sounds          live Ogg Opus registry (fsnotify + rescan, OpusHead sniff, settle check)
internal/voice           join -> DAVE-ready -> suspense -> play -> leave, and channel selection
internal/events          buffered stats log, batched through the store
internal/rollup          refreshes the store's rolled-up analytics every 15 minutes
internal/bot             the Discord client: intents, cache policy, DAVE, the play pool, the guild
                         worker, and the five-minute scheduler that picks somewhere to play
internal/commands        slash commands: definitions, dispatcher, handlers, and their deploy
internal/metrics         Prometheus metrics, served on -http-addr with /healthz + /readyz
internal/tracing         OpenTelemetry setup; off unless -otlp-endpoint is given
pkg/run                  process runner: start concurrently, stop in reverse on signal or error
docker/                  the scratch image build
```

## Database

`internal/store` is one interface; the backend is chosen by the `DATABASE_URL` scheme:

- `postgres://` / `postgresql://` → `store/pg`
- `sqlite://`, `sqlite:`, `file:` → `store/sqlite`

Backends register themselves in `init()`; `internal/app` blank-imports the ones to compile in.
**goose** owns each backend's schema (embedded, applied at boot, Postgres under an advisory lock).
**sqlc** owns each backend's queries, one engine per backend. The SQL is allowed to differ
completely — Postgres batches with `unnest` + `COPY`, SQLite loops single-row inserts in one
transaction. Adding a column means two migrations and two query edits; the reward is
`DATABASE_URL=sqlite:///var/lib/coucou/coucou.db` for a single-box deploy with no database to run.

`make gen` after touching any `.sql`, and commit `gen/`. CI fails if regenerating produces a diff.

**Analytics.** On Postgres the `stats` schema carries views for ad-hoc SQL and Grafana as well as
for the bot: `stats.listens` (a listener joined to their play), `stats.play_outcomes` (played /
fakeout / failed), and `stats.plays_hourly` / `stats.listeners_hourly`, which read UTC-hour rollups
held in materialized views (`*_mv`) plus a live count of every play the rollup has not seen yet —
so they are exact whenever you query them, and `internal/rollup` only decides how much is counted
on read. SQLite counts the same series on read.

Both backends are held to the same behavioural suite in `internal/store/store_test.go`. SQLite runs
always; Postgres runs when `TEST_DATABASE_URL` is set:

```sh
TEST_DATABASE_URL='postgres://user:pass@localhost:5432/scratch?sslmode=disable' go test ./internal/store/
```

## Direct calls

There is no event bus. What happens is recorded by calling `events.Log` where it happens: a play
records itself when it ends, a command when it runs, a sound change when the registry sees it
(after boot — the first scan only prints), a boot reconcile for each guild it finds gone.

Two things are not plain calls:

- **The play pool** (`bot.Pool`) caps simultaneous voice connections at eight across the whole
  bot. A full pool makes the caller wait; a visit runs detached from its caller's cancellation.
- **The guild worker** (`bot.GuildWorker`) takes live joins and leaves off disgo's dispatch
  goroutine and runs them one at a time, in order: a join records itself, syncs the database
  (retried for up to 30 s), queues the persona and posts the welcome. Only the sync retries — the
  boot reconcile repairs a write that gave up, and a retried welcome could post twice. The boot
  burst never reaches it; `SyncGuilds` reconciles that from the cache.

Shutdown runs in reverse registration order: the shard manager closes, then the guild worker
finishes its job in hand, then the pool drains, then the stats log writes its last batch — so a
play cut off by shutdown is still recorded.

## Memory

The whole design serves the RSS target: intents are `Guilds | GuildVoiceStates` only; the cache
holds guilds, roles, voice states, **voice-type channels only**, and only members that are the bot
itself or currently in voice; gateway compression is off and `large_threshold` is 50; channel
selection is cache-only with no REST calls; sounds are pre-encoded so there is no encoder in the
process. `GOMEMLIMIT` and `GOGC` are read from the environment.
