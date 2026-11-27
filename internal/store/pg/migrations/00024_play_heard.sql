-- +goose Up
-- heard: some clip of the visit played. ok stays "the visit finished", so a visit cut short is
-- heard but not ok: it counts as a play everywhere and still as a failure in the fail rate.
-- Materialized views can't be altered, so the rollups and the views over them are rebuilt from
-- 00017 with heard where they counted ok, and ok kept only for failed.
alter table stats.plays add column heard boolean not null default false;
update stats.plays p set heard = exists (select 1 from stats.play_sounds s where s.play_id = p.id and s.played);

drop view stats.listeners_hourly;
drop view stats.plays_hourly;
drop materialized view stats.listeners_hourly_mv;
drop materialized view stats.plays_hourly_mv;

create or replace view stats.listens as
select l.play_id, p.at, p.guild_id, p.channel_id, p.sound, p.trigger, l.user_id, p.ok, l.fled, p.heard
from stats.play_listeners l join stats.plays p on p.id = l.play_id;

create materialized view stats.plays_hourly_mv as
select p.guild_id,
       date_trunc('hour', p.at, 'UTC')                                               as hour,
       count(*) filter (where p.heard)::int                                          as plays,
       count(*) filter (where not p.ok and p.reason is distinct from 'fakeout')::int as failed,
       count(*) filter (where p.reason = 'fakeout')::int                             as fakeouts,
       count(*) filter (where p.heard and p.trigger = 'loop')::int                   as loops,
       count(*) filter (where p.heard and p.trigger = 'command')::int                as commands,
       count(*) filter (where p.heard and p.trigger = 'encore')::int                 as encores,
       coalesce(sum(p.listeners) filter (where p.heard), 0)::int                     as listeners,
       max(p.id)                                                                     as last_play
from stats.plays p
group by 1, 2;
create unique index plays_hourly_mv_key on stats.plays_hourly_mv (guild_id, hour);
create index plays_hourly_mv_last_play on stats.plays_hourly_mv (last_play);

create materialized view stats.listeners_hourly_mv as
select x.guild_id, x.user_id, x.hour,
       sum(x.heard)::int as heard, sum(x.fled)::int as fled, sum(x.triggered)::int as triggered,
       max(x.play_id) as last_play
from (
  select l.play_id, l.guild_id, l.user_id, date_trunc('hour', l.at, 'UTC') as hour,
         l.heard::int as heard, l.fled::int as fled, 0 as triggered
  from stats.listens l
  union all
  select p.id, p.guild_id, p.user_id, date_trunc('hour', p.at, 'UTC'), 0, 0, 1
  from stats.plays p
  where p.trigger = 'command' and p.heard and p.user_id is not null
) x
group by 1, 2, 3
having sum(x.heard) + sum(x.fled) + sum(x.triggered) > 0;
create unique index listeners_hourly_mv_key on stats.listeners_hourly_mv (user_id, hour, guild_id);
create index listeners_hourly_mv_last_play on stats.listeners_hourly_mv (last_play);

create view stats.plays_hourly as
select x.guild_id, x.hour,
       sum(x.plays)::int as plays, sum(x.failed)::int as failed, sum(x.fakeouts)::int as fakeouts,
       sum(x.loops)::int as loops, sum(x.commands)::int as commands, sum(x.encores)::int as encores,
       sum(x.listeners)::int as listeners
from (
  select m.guild_id, m.hour, m.plays, m.failed, m.fakeouts, m.loops, m.commands, m.encores, m.listeners
  from stats.plays_hourly_mv m
  union all
  select p.guild_id,
         date_trunc('hour', p.at, 'UTC'),
         (p.heard)::int,
         (not p.ok and p.reason is distinct from 'fakeout')::int,
         (p.reason is not distinct from 'fakeout')::int,
         (p.heard and p.trigger = 'loop')::int,
         (p.heard and p.trigger = 'command')::int,
         (p.heard and p.trigger = 'encore')::int,
         case when p.heard then p.listeners else 0 end
  from stats.plays p
  where p.id > coalesce((select max(h.last_play) from stats.plays_hourly_mv h), 0)
) x
group by 1, 2;

create view stats.listeners_hourly as
select x.guild_id, x.user_id, x.hour,
       sum(x.heard)::int as heard, sum(x.fled)::int as fled, sum(x.triggered)::int as triggered
from (
  select m.guild_id, m.user_id, m.hour, m.heard, m.fled, m.triggered
  from stats.listeners_hourly_mv m
  union all
  select l.guild_id, l.user_id, date_trunc('hour', l.at, 'UTC'), l.heard::int, l.fled::int, 0
  from stats.listens l
  where l.play_id > coalesce((select max(h.last_play) from stats.listeners_hourly_mv h), 0)
  union all
  select p.guild_id, p.user_id, date_trunc('hour', p.at, 'UTC'), 0, 0, 1
  from stats.plays p
  where p.trigger = 'command' and p.heard and p.user_id is not null
    and p.id > coalesce((select max(h.last_play) from stats.listeners_hourly_mv h), 0)
) x
group by 1, 2, 3
having sum(x.heard) + sum(x.fled) + sum(x.triggered) > 0;

-- +goose Down
-- Back to the 00017 definitions, which count ok.
drop view stats.listeners_hourly;
drop view stats.plays_hourly;
drop materialized view stats.listeners_hourly_mv;
drop materialized view stats.plays_hourly_mv;
drop view stats.listens;

create view stats.listens as
select l.play_id, p.at, p.guild_id, p.channel_id, p.sound, p.trigger, l.user_id, p.ok, l.fled
from stats.play_listeners l join stats.plays p on p.id = l.play_id;

create materialized view stats.plays_hourly_mv as
select p.guild_id,
       date_trunc('hour', p.at, 'UTC')                                               as hour,
       count(*) filter (where p.ok)::int                                             as plays,
       count(*) filter (where not p.ok and p.reason is distinct from 'fakeout')::int as failed,
       count(*) filter (where p.reason = 'fakeout')::int                             as fakeouts,
       count(*) filter (where p.ok and p.trigger = 'loop')::int                      as loops,
       count(*) filter (where p.ok and p.trigger = 'command')::int                   as commands,
       count(*) filter (where p.ok and p.trigger = 'encore')::int                    as encores,
       coalesce(sum(p.listeners) filter (where p.ok), 0)::int                        as listeners,
       max(p.id)                                                                     as last_play
from stats.plays p
group by 1, 2;
create unique index plays_hourly_mv_key on stats.plays_hourly_mv (guild_id, hour);
create index plays_hourly_mv_last_play on stats.plays_hourly_mv (last_play);

create materialized view stats.listeners_hourly_mv as
select x.guild_id, x.user_id, x.hour,
       sum(x.heard)::int as heard, sum(x.fled)::int as fled, sum(x.triggered)::int as triggered,
       max(x.play_id) as last_play
from (
  select l.play_id, l.guild_id, l.user_id, date_trunc('hour', l.at, 'UTC') as hour,
         l.ok::int as heard, l.fled::int as fled, 0 as triggered
  from stats.listens l
  union all
  select p.id, p.guild_id, p.user_id, date_trunc('hour', p.at, 'UTC'), 0, 0, 1
  from stats.plays p
  where p.trigger = 'command' and p.ok and p.user_id is not null
) x
group by 1, 2, 3
having sum(x.heard) + sum(x.fled) + sum(x.triggered) > 0;
create unique index listeners_hourly_mv_key on stats.listeners_hourly_mv (user_id, hour, guild_id);
create index listeners_hourly_mv_last_play on stats.listeners_hourly_mv (last_play);

create view stats.plays_hourly as
select x.guild_id, x.hour,
       sum(x.plays)::int as plays, sum(x.failed)::int as failed, sum(x.fakeouts)::int as fakeouts,
       sum(x.loops)::int as loops, sum(x.commands)::int as commands, sum(x.encores)::int as encores,
       sum(x.listeners)::int as listeners
from (
  select m.guild_id, m.hour, m.plays, m.failed, m.fakeouts, m.loops, m.commands, m.encores, m.listeners
  from stats.plays_hourly_mv m
  union all
  select p.guild_id,
         date_trunc('hour', p.at, 'UTC'),
         (p.ok)::int,
         (not p.ok and p.reason is distinct from 'fakeout')::int,
         (p.reason is not distinct from 'fakeout')::int,
         (p.ok and p.trigger = 'loop')::int,
         (p.ok and p.trigger = 'command')::int,
         (p.ok and p.trigger = 'encore')::int,
         case when p.ok then p.listeners else 0 end
  from stats.plays p
  where p.id > coalesce((select max(h.last_play) from stats.plays_hourly_mv h), 0)
) x
group by 1, 2;

create view stats.listeners_hourly as
select x.guild_id, x.user_id, x.hour,
       sum(x.heard)::int as heard, sum(x.fled)::int as fled, sum(x.triggered)::int as triggered
from (
  select m.guild_id, m.user_id, m.hour, m.heard, m.fled, m.triggered
  from stats.listeners_hourly_mv m
  union all
  select l.guild_id, l.user_id, date_trunc('hour', l.at, 'UTC'), l.ok::int, l.fled::int, 0
  from stats.listens l
  where l.play_id > coalesce((select max(h.last_play) from stats.listeners_hourly_mv h), 0)
  union all
  select p.guild_id, p.user_id, date_trunc('hour', p.at, 'UTC'), 0, 0, 1
  from stats.plays p
  where p.trigger = 'command' and p.ok and p.user_id is not null
    and p.id > coalesce((select max(h.last_play) from stats.listeners_hourly_mv h), 0)
) x
group by 1, 2, 3
having sum(x.heard) + sum(x.fled) + sum(x.triggered) > 0;

alter table stats.plays drop column heard;
