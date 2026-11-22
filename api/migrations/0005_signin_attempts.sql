-- +goose Up
-- Failed sign-in attempts, counted per typed email and per IP address to slow down guessing (spec 02).
-- The key is the email as typed, not the account, so an unknown address locks exactly like a real one.

create table signin_attempt (
    id  bigserial primary key,
    key text not null,
    at  timestamptz not null
);
create index signin_attempt_key_idx on signin_attempt (key, at);

grant select, insert, delete on signin_attempt to app;
grant usage on sequence signin_attempt_id_seq to app;

-- +goose Down
drop table signin_attempt;
