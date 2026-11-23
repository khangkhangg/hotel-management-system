-- +goose Up
-- Rate limits are kept here so every server shares them. The rows hold an address and a purpose, no hotel data.
create table rate_hit (
  key text not null,
  at  timestamptz not null
);
create index rate_hit_key_at on rate_hit (key, at);
create index rate_hit_at on rate_hit (at);
grant select, insert, delete on rate_hit to app;

-- +goose Down
drop table rate_hit;
