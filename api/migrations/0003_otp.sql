-- +goose Up
-- Email codes for the second sign-in step (spec 02). Only a hash of each code is stored.

create table otp_challenge (
    id           uuid primary key,
    user_id      uuid not null references app_user (id),
    code_hash    bytea not null,
    created_at   timestamptz not null,
    expires_at   timestamptz not null,
    attempts     int not null default 0,
    used_at      timestamptz,
    cancelled_at timestamptz
);
create index otp_challenge_user_idx on otp_challenge (user_id, created_at desc);

grant select, insert, update, delete on otp_challenge to app;

-- +goose Down
drop table otp_challenge;
