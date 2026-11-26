# Chain Graph Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace linear `[[chains]]` with a weighted sound graph walked hop by hop under a global, decaying chance and a hard hop cap.

**Architecture:** `sounds.Chains` holds the global knobs and an adjacency map; `Registry.Follow` walks it. `profile` parses and validates `[chaining]` + `[[chains]]` into that type. The single caller (`bot.go:233`) and the `Arrange` calls in `characters.go` keep their shape.

**Tech Stack:** Go stdlib plus the TOML library `profile` already uses.

**Spec:** `docs/superpowers/specs/2026-11-26-chain-graph-design.md`

## Global Constraints

- Odds at hop *n* (0-based) = `chance × decay^n`, rolled as `intN(100) < odds`.
- `chance` 0-100 default 100; `decay` 0-1 default 1 (NaN refused); `max_hops` 1-20 default 8.
- Edge `weight` > 0; edge `after` 0-20 s, rounded to the ms (reuse `MaxAfter`).
- Cycles allowed. Every sound can be drawn first: `followers` is deleted.
- Commits as be-sandaa, Conventional Commits, no AI trailer.

## Review Focus

1. `decay = 0`: hop 0 still rolls at full `chance` (0⁰ = 1), every later hop stops. → Task 1 "decay 0 stops after one".
2. Self-loop with `chance = 100, decay = 1`: exactly `max_hops` clips. → Task 1 "loop capped".
3. Every child of a node missing or NSFW-blocked: walk ends, no roll spent. → Task 1 "no playable child".
4. `[[chains]]` with no `[chaining]` table: defaults 100 / 1 / 8 apply. → Task 2 valid case.
5. `decay = nan` / `max_hops = 0`: refused at load. → Task 2 invalid cases.
6. Existing play history after the migration: top sound and heard sounds must not drop to empty. → Task 4 backfill test.

---

### Task 1: Graph walk in `sounds`

**Files:**
- Modify: `internal/sounds/sounds.go:83-122` (types, fields, `Arrange`), `:291` and `:354` (drop `followers`), `:362-378` (`Follow`)
- Test: `internal/sounds/sounds_test.go:601-655` (`chained`, `TestFollow`, `TestFollowersAreNotDrawn`)

**Interfaces:**
- Produces:
  ```go
  type Edge struct { Sound string; Weight int; After time.Duration }
  type Chains struct {
      Chance  int               // % at hop 0
      Decay   float64           // multiplier per hop
      MaxHops int
      Next    map[string][]Edge // by sound, edges in profile order
  }
  func (r *Registry) Arrange(chains Chains, links Links)
  func (r *Registry) Follow(opener string, nsfw bool) []Clip // signature unchanged
  ```
  `Step`, `Chain` and the `followers` field are deleted; the registry field becomes `chains Chains`.

- [ ] **Step 1: Rewrite the tests.** `chained` arranges `Chains{Chance: 50, Decay: 0.5, MaxHops: 3, Next: {knock: {{who, 1, 2s}, {"missing", 5, 0}, {"n", 3, 0}}, who: {{knock, 1, 1s}}}}` plus the existing links. Each hop spends one `intN(100)` roll, then one `intN(totalWeight)` pick. `TestFollow` cases:
  - "no chain": `drum` → nil, no rolls.
  - "the roll fails": `knock`, rolls `[50]` → nil.
  - "missing and nsfw children left out": `knock`, nsfw=false, rolls `[0, 0, 25]` → `[who 2s]` (hop 1 odds 25, roll 25 stops).
  - "walks a loop": `knock`, nsfw=false, rolls `[0, 0, 24, 0, 12, 0]` → `[who 2s, knock 1s, who 2s]` (hop 2 odds 12.5; MaxHops 3 then stops without a roll).
  - "nsfw child weighted in": `knock`, nsfw=true, rolls `[0, 1]` → `[n]` (weights who 1, n 3; `n` has no entry).
  - "decay 0 stops after one": same graph with `Decay: 0`, rolls `[0, 0, 0]` → `[who 2s]`.
  - "loop capped": `Next{knock: {{knock, 1, 0}}}`, `Chance: 100, Decay: 1, MaxHops: 4`, all rolls 0 → exactly 4 `knock` clips.
  - "no playable child": `Next{knock: {{"missing", 1, 0}}}`, no rolls scripted → nil (scripted fails the test if called).
  Replace `TestFollowersAreNotDrawn` with `TestChainedSoundsAreDrawn`: over 50 `Pick` calls on `chained(t, knock, who)`, both names come up.

- [ ] **Step 2: Run** `go test ./internal/sounds/` — expect compile failure (`Chains`, `Edge` undefined).

- [ ] **Step 3: Implement.** `Follow` under `r.mu`:
  ```go
  cur := opener
  for hop := 0; hop < c.MaxHops; hop++ {
      edges := children of cur whose file exists and is allowed(nsfw) // before any roll
      if len(edges) == 0 { break }
      if float64(r.intN(100)) >= float64(c.Chance)*math.Pow(c.Decay, float64(hop)) { break }
      e := weighted pick with r.intN(sum of weights), in slice order
      out = append(out, Clip{Name: e.Sound, Path: r.files[e.Sound].path, After: e.After})
      cur = e.Sound
  }
  ```
  Delete the `followers` checks from `Pick` and `PickOther`; update the doc comments on `Follow` and the registry field.

- [ ] **Step 4: Run** `go test -race ./internal/sounds/` — PASS.

- [ ] **Step 5:** No commit yet: `profile` does not build until Task 2. Both tasks land in one commit.

### Task 2: Parse and validate the graph in `profile`

**Files:**
- Modify: `internal/profile/profile.go:35-45` (add `MaxHops = 20`), `:73` (`Chains sounds.Chains`), `:130-136` (file struct), `:292-324` (`chains()`)
- Modify: `profile.example.toml:26-35`, `docs/wiki/reference/Profile.md:26`
- Test: `internal/profile/profile_test.go:68-96,131-139`

**Interfaces:**
- Consumes: `sounds.Chains`, `sounds.Edge` from Task 1.
- Produces: `Profile.Chains sounds.Chains`; `const MaxHops = 20`. `characters.go:62,186` compile unchanged.

- [ ] **Step 1: Rewrite the tests.** The valid profile uses:
  ```toml
  [chaining]
  chance = 80
  decay = 0.7
  max_hops = 5
  [[chains]]
  sound = "knock"
  next = [{ sound = "who", weight = 70, after = 2 }, { sound = "rim", weight = 30 }]
  [[chains]]
  sound = "who"
  next = [{ sound = "knock", weight = 1, after = 0.35 }]
  ```
  want `Chains{Chance: 80, Decay: 0.7, MaxHops: 5, Next: {"knock": {{"who", 70, 2s}, {"rim", 30, 0}}, "who": {{"knock", 1, 350ms}}}}`. Add: `[[chains]]` without `[chaining]` → `Chance 100, Decay 1, MaxHops 8`; neither → zero `Chains{}`. Invalid cases (replace lines 131-139): chance 101, chance -1, decay 1.1, decay -0.1, decay nan, max_hops 0, max_hops 21, chain without sound, empty `next`, edge without sound, edge weight 0, edge after 21 / 20.001 / -0.2 / nan, unknown edge key `delay`, old key `steps`, two `[[chains]]` for one sound.

- [ ] **Step 2: Run** `go test ./internal/profile/` — FAIL.

- [ ] **Step 3: Implement** `func (f file) chains() (sounds.Chains, error)`. File struct gains `Chaining struct { Chance *int; Decay *float64; MaxHops *int }` (toml `chance`, `decay`, `max_hops`) and `Chains []struct { Sound string; Next []struct { Sound string; Weight int; After float64 } }`. Knobs are validated whenever set; `Next` stays nil and defaults are skipped when there are no `[[chains]]`. Decay check `!(d >= 0 && d <= 1)` refuses NaN; keep the existing `after` check and ms rounding. Error keys follow the existing style: `chaining.decay`, `chains[%d].next[%d].weight`. Update `profile.example.toml` and the `Profile.md` table (`[chaining]` row, `[[chains]]` row) to the spec's config block.

- [ ] **Step 4: Run** `go test -race ./... && make lint` — PASS, clean.

- [ ] **Step 5: Commit** `feat(chains): walk chains as a weighted graph with decaying odds` (code, tests, example, wiki, spec, plan).

### Task 3: Preload a visit's clips and play them as one stream

Replaces the per-clip `os.Open` + provider swap + wall-clock `sleep(gap)` with one provider over packets demuxed before joining. (User's pick, option B.)

**Files:**
- Modify: `internal/voice/voice.go:47-51` (`Clip` unchanged), `:107-153` (`Play`: load before `busy`/join), `:155-203` (`playClips`, `playClip` → stream)
- Modify: `internal/voice/voice.go:255-272` (`notifyingProvider` is subsumed by the stream; delete it and its test)
- Test: `internal/voice/voice_test.go`

**Interfaces:**
- Consumes: `newOggOpusReader(io.Reader)` from `ogg.go`; `voice.SilenceAudioFrame` from disgo.
- Produces (package-private):
  ```go
  func load(clips []Clip) (*stream, error) // reads + demuxes every file; any error fails the whole visit
  type stream struct { /* packets [][][]byte; gaps []int (silence frames before clip i); ended chan int (cap len(clips)) */ }
  func (s *stream) ProvideOpusFrame() ([]byte, error) // clip 0 packets, gap[1] silence frames, clip 1 packets, …, then silence forever
  func (s *stream) Close()
  ```
  `Play`, `Clip`, `Opts`, `ClipTimeout`, `ErrPlayTimeout` keep their exported shape, so `bot.go` and its `timeout()` are untouched.

- [ ] **Step 1: Write the failing tests** (fixture: build tiny Ogg Opus bytes the way `ogg_test.go` already does, written to `t.TempDir()`):
  - `TestStreamOrdersClipsAndGaps`: clips A (packets `a1,a2`), B (`b1`, Gap 60ms) → frames `a1, a2, S, S, S, b1, S, S…` where S = `SilenceAudioFrame`; `ended` receives 0 after `a2` is consumed and 1 after `b1`.
  - `TestStreamGapRoundsToFrames`: Gap 50ms → 3 silence frames (`ceil(gap/20ms)`, so a gap is never shorter than asked).
  - `TestStreamZeroGapIsBackToBack`: Gap 0 → `a2` is followed directly by `b1`, no silence.
  - `TestLoadFailsOnAMissingLaterClip`: second clip path missing → `load` returns an error wrapping `fs.ErrNotExist`.
  - `TestStreamNeverEndsWithEmptyFrame`: after the last clip, 10 calls all return `SilenceAudioFrame, nil` (keeps the disgo v0.19.6 SetSpeaking deadlock guard the old provider documented).

- [ ] **Step 2: Run** `go test ./internal/voice/` — FAIL (undefined `load`, `stream`).

- [ ] **Step 3: Implement.**
  - `load` drains each `newOggOpusReader` until `io.EOF` into a packet slice; error is `fmt.Errorf("load %s: %w", file, err)`.
  - `Play` calls `load` first, before `busy.LoadOrStore`; a failure returns `StagePlay, err` without joining (my call: it is the clip that cannot play, and the outcome column is free text).
  - `playClips(ctx, conn, s *stream, clips, stillPopulated)`: one `conn.SetOpusFrameProvider(s)`, then for each clip wait on `s.ended` with a timer of `clips[i].Gap + ClipTimeout` (the same budget `bot.timeout()` grants) → `ErrPlayTimeout`, or `ctx.Done()`. After clip *i* < last ends, `stillPopulated() == false` returns nil early, as today. `ended` sends are non-blocking-safe because the channel holds `len(clips)`.
  - Keep the 100ms drain sleep after the last clip.

- [ ] **Step 4: Run** `go test -race ./... && make lint` — PASS, clean.

- [ ] **Step 5: Commit** `feat(voice): preload a visit's clips and play them as one stream`.

### Task 4: Store every clip of a visit, enough to replay it

Today a visit is one `stats_plays` row whose `sound` is the opener; followers are lost. (User's pick: child table, every planned clip with a played flag, plus suspense.)

**Files:**
- Create: `internal/store/sqlite/migrations/00021_play_sounds.sql`, `internal/store/pg/migrations/00023_play_sounds.sql` (goose; pg holds the comment, sqlite points to it, as in the `characters` pair)
- Modify: `internal/voice/voice.go` (`Play` returns how many clips finished), `internal/bot/play.go:89-130`, `internal/bot/bot.go:226-240`, `internal/bus/events.go:15-28`, `internal/bus/handlers/handlers.go:37-50`, `internal/store/store.go:47-60`
- Modify: `internal/store/sqlite/queries/queries.sql` (`InsertPlaySound`, top sound ×2, `HeardSounds`, `BoardSounds`), `internal/store/pg/queries/events.sql` (`InsertPlaySounds :copyfrom`, `suspense_ms` in `InsertPlays`), `internal/store/pg/queries/stats.sql` (same four reads), `internal/store/sqlite/sqlite.go:174-191`, `internal/store/pg/pg.go:172-212`; regenerate `gen/` with `make sqlc`
- Test: `internal/store/store_test.go`, `internal/store/sqlite/migrate_test.go`, `internal/bot/play_test.go`

**Schema (both dialects):**
```sql
create table stats_play_sounds (          -- pg: stats.play_sounds
  play_id  integer not null references stats_plays(id) on delete cascade,
  position integer not null,              -- 0 = opener
  sound    text    not null,
  gap_ms   integer not null,              -- 0 at position 0
  played   integer not null,              -- pg: boolean
  primary key (play_id, position)
);                                         -- sqlite: without rowid, like stats_play_listeners
alter table stats_plays add column suspense_ms integer;  -- null: recorded before this column
insert into stats_play_sounds select id, 0, sound, 0, ok from stats_plays;  -- backfill, so history keeps counting
```

**Interfaces:**
- Consumes: Task 3's stream, which knows how many clips ended.
- Produces:
  ```go
  // voice
  func Play(ctx, client, guild, channel, clips []Clip, o Opts) (stage string, played int, err error)
  // store (events.Play aliases it)
  type PlaySound struct { Sound string; Gap time.Duration; Played bool }
  // store.Play gains: Sounds []PlaySound; Suspense time.Duration
  // bus.PlayFinished gains: Sounds []store.PlaySound `json:"sounds"`; Suspense time.Duration `json:"suspense"`
  ```
  `bot.go` builds the planned `[]PlaySound` alongside `clips` (opener + each `Follow` clip, names and gaps); `play()` takes it and marks the first `played` entries `Played: true`. A fake-out records the opener with `Played: false`.

- [ ] **Step 1: Write the failing tests.**
  - `store_test.go` `TestPlaySoundsRoundTrip` (against whichever stores the file already covers): write a play with sounds `[knock 0 true, who 2s true, rim 0 false]` and `Suspense 3s`; `HeardSounds` for a listener returns `knock, who`, not `rim`; `BoardSounds` counts `who`.
  - `migrate_test.go`: a `stats_plays` row inserted before `00021` gets one `stats_play_sounds` row `(0, sound, 0, ok)` after it.
  - `play_test.go`: `played = 2` of 3 planned → `PlayFinished.Sounds` flags `true, true, false`; fake-out → `[{opener, 0, false}]`.

- [ ] **Step 2: Run** `go test ./internal/store/... ./internal/bot/` — FAIL.

- [ ] **Step 3: Implement.** Migrations as above. Inserts: sqlite one `InsertPlaySound` per sound inside the existing `tx`; pg `InsertPlaySounds` via COPY after `InsertPlays`, keyed by the returned ids like listeners; `suspense_ms` joins `InsertPlays`' unnest list as `int[]`. Reads: top sound, `HeardSounds` and `BoardSounds` join `stats_play_sounds s on s.play_id = p.id and s.played`, group by `s.sound`, keep their existing `p.ok` and window filters. Visit counts untouched. `make sqlc`.

- [ ] **Step 4: Run** `go test -race ./... && make lint` — PASS, clean.

- [ ] **Step 5: Commit** `feat(store): record every clip of a visit and its suspense`, then push all three commits as be-sandaa.
