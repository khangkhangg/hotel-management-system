-- +goose Up
-- A chain sets a rate plan once and each property links to it (spec 03). A linked plan follows the template.
create table rate_plan_template (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  name text not null check (btrim(name) <> ''),
  free_cancel_until_days integer check (free_cancel_until_days >= 0),
  free_cancel_until_time time,
  late_cancel_penalty text not null default 'none' check (late_cancel_penalty in ('none', 'first_night', 'percent', 'full')),
  late_cancel_percent integer check (late_cancel_percent between 1 and 100),
  meal_plan text not null default 'none',
  created_at timestamptz not null,
  unique (id, organization_id),
  unique (organization_id, name)
);
alter table rate_plan_template enable row level security;
alter table rate_plan_template force row level security;
create policy rate_plan_template_isolation on rate_plan_template using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on rate_plan_template to app;
alter table rate_plan add foreign key (template_id, organization_id) references rate_plan_template (id, organization_id);

-- +goose Down
alter table rate_plan drop constraint rate_plan_template_id_organization_id_fkey;
drop table rate_plan_template;
