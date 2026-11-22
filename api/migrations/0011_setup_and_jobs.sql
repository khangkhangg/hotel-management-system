-- +goose Up
-- The nightly check needs to find every organization that has an activity trail. Row-level security shows the
-- API role one organization at a time, so this function, which runs as the table owner, lists them.
-- +goose StatementBegin
create function audit_organizations() returns setof uuid
language sql stable security definer set search_path = public as $$
  select distinct organization_id from audit_log
$$;
-- +goose StatementEnd
revoke all on function audit_organizations() from public;
grant execute on function audit_organizations() to app;

-- The very first owner is invited by "server setup", before any person exists to do the inviting.
alter table invite alter column invited_by drop not null;

-- One row per owner email that "server setup" has already used, so running it twice cannot create a second organization.
create table setup_owner (
  email text primary key check (email = lower(btrim(email))),
  created_at timestamptz not null
);
grant select, insert on setup_owner to app;

-- +goose Down
drop table setup_owner;
alter table invite alter column invited_by set not null;
drop function audit_organizations();
