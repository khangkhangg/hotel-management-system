-- +goose Up
-- Sessions and trusted devices (spec 02). Person-level tables: no organization, no row-level security.
-- Only a SHA-256 hash of each id and token is stored, so a database leak does not hand out live sessions.

create table session (
    id_hash      bytea primary key,
    user_id      uuid not null references app_user (id),
    stage        text not null check (stage in ('pending', 'full')),
    org_id       uuid references organization (id),
    view_as      text not null default '' check (view_as in ('', 'manager', 'front_desk', 'revenue', 'housekeeping')),
    device_label text not null default '',
    created_at   timestamptz not null,
    last_seen_at timestamptz not null
);
create index session_user_idx on session (user_id);

create table trusted_device (
    token_hash bytea primary key,
    user_id    uuid not null references app_user (id),
    expires_at timestamptz not null,
    created_at timestamptz not null
);
create index trusted_device_user_idx on trusted_device (user_id);

grant select, insert, update, delete on session, trusted_device to app;

-- +goose Down
drop table trusted_device;
drop table session;
