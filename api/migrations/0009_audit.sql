-- +goose Up
-- The activity trail (spec 06). One hash chain per organization: each row stores the hash of the row before it,
-- so removing or editing a row breaks every hash after it.
create table audit_log (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  seq bigint not null,
  occurred_at timestamptz not null,
  actor_user_id uuid references app_user (id),
  actor_role text not null,
  view_as text not null default '',
  property_id uuid,
  action text not null,
  target_type text not null,
  target_id uuid,
  target_label text not null default '',
  before jsonb,
  after jsonb,
  reason text not null default '',
  device text not null default '',
  ip text not null default '',
  place text not null default '',
  trace_id uuid,
  prev_hash bytea not null,
  row_hash bytea not null,
  unique (organization_id, seq)
);
create index audit_log_target_idx on audit_log (organization_id, target_type, target_id, seq desc);
create index audit_log_actor_idx on audit_log (organization_id, actor_user_id, seq desc);

-- Not FORCEd on purpose: the owning role (migrator) can still repair or inspect rows, while the API role
-- (app) is bound by the policy and holds no update or delete grant at all.
alter table audit_log enable row level security;
create policy audit_isolation on audit_log
  using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert on audit_log to app;

-- +goose Down
drop table audit_log;
