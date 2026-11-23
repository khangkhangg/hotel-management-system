-- +goose Up
-- Yield rules change what is sent to the booking sites: how many rooms and at what price. The direct booking page is not touched.
create table yield_rule (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  kind            text not null check (kind in ('auto_close', 'sell_limit', 'availability_cap', 'rate_adjust')),
  room_type_id    uuid,
  from_date       date,
  to_date         date,
  value           integer not null check (value >= 0),
  percent         integer not null default 0 check (percent between -50 and 100),
  active          boolean not null default true,
  created_at      timestamptz not null default now(),
  foreign key (property_id, organization_id) references property (id, organization_id),
  check (from_date is null or to_date is null or from_date <= to_date)
);
create index yield_rule_property on yield_rule (property_id);

alter table yield_rule enable row level security;
alter table yield_rule force row level security;
create policy yield_rule_isolation on yield_rule using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update, delete on yield_rule to app;

-- +goose Down
drop table yield_rule;
