-- +goose Up
-- Alerts and the per-property settings of the checks that raise them (spec 09).
create table alert (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  check_kind text not null,
  subject_type text not null,
  subject_id uuid not null,
  severity text not null check (severity in ('urgent', 'warning', 'info')),
  reason jsonb not null,
  suggested_action jsonb,
  status text not null default 'open' check (status in ('open', 'accepted', 'dismissed', 'snoozed', 'closed')),
  target_role text not null check (target_role in ('owner', 'manager', 'front_desk', 'revenue', 'housekeeping')),
  opened_at timestamptz not null,
  closed_at timestamptz,
  closed_by uuid references app_user (id),
  close_reason text not null default '',
  snoozed_until timestamptz,
  foreign key (property_id, organization_id) references property (id, organization_id),
  unique (id, organization_id)
);
-- One problem, one alert: the same check and subject cannot be open or snoozed twice.
create unique index alert_open_idx on alert (property_id, check_kind, subject_id) where status in ('open', 'snoozed');
create index alert_property_idx on alert (property_id, status, opened_at desc);

create table check_setting (
  property_id uuid not null,
  organization_id uuid not null references organization (id),
  check_kind text not null,
  enabled boolean not null default true,
  thresholds jsonb not null default '{}',
  primary key (property_id, check_kind),
  foreign key (property_id, organization_id) references property (id, organization_id)
);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['alert', 'check_setting'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
                      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd

-- The checks run for every property, so the background job needs the list. Row-level security shows one organization at a time,
-- so the property table lets its owner (and this function) read all rows, like audit_log and notification.
alter table property no force row level security;
-- +goose StatementBegin
create function all_properties() returns table (organization_id uuid, property_id uuid)
language sql stable security definer set search_path = public, pg_temp as $$
  select organization_id, id from property
$$;
-- +goose StatementEnd
revoke all on function all_properties() from public;
grant execute on function all_properties() to app;

-- +goose Down
drop function all_properties();
alter table property force row level security;
drop table check_setting, alert;
