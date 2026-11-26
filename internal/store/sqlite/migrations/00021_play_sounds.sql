-- +goose Up
-- See the postgres 00023 migration.
create table stats_play_sounds (
  play_id  integer not null references stats_plays(id) on delete cascade,
  position integer not null,
  sound    text    not null,
  gap_ms   integer not null,
  played   integer not null,
  primary key (play_id, position)
) without rowid;
alter table stats_plays add column suspense_ms integer;
insert into stats_play_sounds (play_id, position, sound, gap_ms, played) select id, 0, sound, 0, ok from stats_plays;

-- +goose Down
alter table stats_plays drop column suspense_ms;
drop table stats_play_sounds;
