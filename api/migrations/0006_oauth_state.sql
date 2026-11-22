-- +goose Up
-- One row per Google sign-in in progress: the PKCE verifier waits here until Google sends the person back.
-- Only a hash of the state is stored. A row is used once and expires after 10 minutes.

create table oauth_state (
    state_hash bytea primary key,
    verifier   text not null,
    expires_at timestamptz not null
);

grant select, insert, delete on oauth_state to app;

-- +goose Down
drop table oauth_state;
