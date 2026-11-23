-- +goose Up
-- When the checks last ran for each property, so an empty panel can say "Nothing needs attention, checked at 14:05".
create table alert_run (
  property_id uuid primary key,
  organization_id uuid not null references organization (id),
  ran_at timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id)
);
alter table alert_run enable row level security;
alter table alert_run force row level security;
create policy alert_run_isolation on alert_run using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on alert_run to app;

-- +goose Down
drop table alert_run;
