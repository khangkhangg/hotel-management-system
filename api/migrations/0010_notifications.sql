-- +goose Up
-- In-app notifications, one row per person, and each person's quiet hours and email choices (spec 06).
-- Preferences are a person's own and hold no secrets, so they have no row-level policy: the only way to reach a
-- preference row is through a user ID that an organization-scoped query has already produced.
create table notification_preference (
  user_id uuid primary key references app_user (id),
  quiet_from time,
  quiet_to time,
  email_urgent boolean not null default true,
  email_action_needed boolean not null default false,
  check ((quiet_from is null) = (quiet_to is null))
);
grant select, insert, update on notification_preference to app;

create table notification (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid,
  user_id uuid not null references app_user (id),
  level text not null check (level in ('urgent', 'action_needed', 'info')),
  key text not null,
  title text not null,
  body text not null default '',
  action_url text not null default '',
  count integer not null default 1,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  opened_at timestamptz,
  resolved_at timestamptz,
  resolved_by uuid references app_user (id),
  email_due_at timestamptz,
  email_sent_at timestamptz,
  escalation smallint not null default 0 -- 0 = not escalated, 1 = managers told, 2 = owners told (or itself an escalation)
);
-- One open notification per person and key: repeats raise its count instead of adding rows.
create unique index notification_open_key on notification (user_id, key) where resolved_at is null;
create index notification_user_idx on notification (user_id, updated_at desc);
create index notification_org_key_idx on notification (organization_id, key);

-- Not FORCEd, like audit_log: the background sweep needs to find which organizations have work due.
-- The API role is still bound by the policy.
alter table notification enable row level security;
create policy notification_isolation on notification
  using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on notification to app;

-- The sweep cannot list organizations itself (row-level security shows one at a time), so this function,
-- which runs as the table owner, names the organizations that have an email to send or an escalation due.
-- +goose StatementBegin
create function notification_orgs_due(at timestamptz) returns setof uuid
language sql stable security definer set search_path = public as $$
  select distinct organization_id from notification
  where resolved_at is null
    and ((email_due_at <= at and email_sent_at is null)
      or (level = 'urgent' and opened_at is null and escalation < 2 and created_at <= at - interval '10 minutes'))
$$;
-- +goose StatementEnd
revoke all on function notification_orgs_due(timestamptz) from public;
grant execute on function notification_orgs_due(timestamptz) to app;

-- +goose Down
drop function notification_orgs_due(timestamptz);
drop table notification;
drop table notification_preference;
