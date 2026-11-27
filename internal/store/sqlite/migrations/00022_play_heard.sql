-- +goose Up
-- See the postgres 00024 migration.
alter table stats_plays add column heard integer not null default 0;
update stats_plays set heard = exists (select 1 from stats_play_sounds s where s.play_id = stats_plays.id and s.played);

-- +goose Down
alter table stats_plays drop column heard;
