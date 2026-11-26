-- name: InsertPlays :many
-- One round-trip for a whole batch. sqlc's postgres catalog only knows single-argument unnest, so the
-- arrays are zipped by one set-returning call per column in an inner select; Postgres steps them in
-- lockstep, so RETURNING ids come back in input order and line up with the arrays.
--
-- sqlc maps bigint[]/text[] to []int64/[]string and has no way to express a nullable *element*, so the
-- nullable columns travel as sentinels (0 / '') and become NULL here, at the insert, not in Go.
insert into stats.plays (at, guild_id, channel_id, sound, trigger, user_id, listeners, ok, reason, duration_ms, character, suspense_ms)
select
  r.at, r.guild_id, r.channel_id, r.sound, r.trigger,
  nullif(r.user_id, 0),
  r.listeners, r.ok,
  nullif(r.reason, ''),
  r.duration_ms,
  nullif(r.character, ''),
  r.suspense_ms
from (
  select
    unnest(sqlc.arg(at)::timestamptz[])        as at,
    unnest(sqlc.arg(guild_ids)::bigint[])      as guild_id,
    unnest(sqlc.arg(channel_ids)::bigint[])    as channel_id,
    unnest(sqlc.arg(sounds)::text[])           as sound,
    unnest(sqlc.arg(triggers)::text[])         as trigger,
    unnest(sqlc.arg(user_ids)::bigint[])       as user_id,
    unnest(sqlc.arg(listeners)::smallint[])    as listeners,
    unnest(sqlc.arg(oks)::boolean[])           as ok,
    unnest(sqlc.arg(reasons)::text[])          as reason,
    unnest(sqlc.arg(durations_ms)::int[])      as duration_ms,
    unnest(sqlc.arg(characters)::text[])       as character,
    unnest(sqlc.arg(suspenses_ms)::int[])      as suspense_ms
) r
returning id;

-- name: InsertPlaySounds :copyfrom
-- Every clip of a visit, opener at position 0; the primary key refuses a position twice.
insert into stats.play_sounds (play_id, position, sound, gap_ms, played) values ($1, $2, $3, $4, $5);

-- name: InsertPlayListeners :copyfrom
insert into stats.play_listeners (play_id, user_id, fled) values ($1, $2, $3);

-- name: InsertEvent :exec
insert into audit.events (at, kind, guild_id, user_id, data) values ($1, $2, $3, $4, $5);
