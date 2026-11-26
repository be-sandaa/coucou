-- +goose Up
-- Every clip a visit planned, in order, and whether it played: with suspense_ms, enough to replay the
-- visit. plays.sound stays the opener. Each play from before this gets its one sound, so heard and
-- top sounds, which now read this table, keep their history.
create table stats.play_sounds (
  play_id  bigint   not null references stats.plays(id) on delete cascade,
  position smallint not null,
  sound    text     not null,
  gap_ms   integer  not null,
  played   boolean  not null,
  primary key (play_id, position)
);
alter table stats.plays add column suspense_ms integer; -- null: recorded before this column
insert into stats.play_sounds (play_id, position, sound, gap_ms, played) select id, 0, sound, 0, ok from stats.plays;

-- +goose Down
alter table stats.plays drop column suspense_ms;
drop table stats.play_sounds;
