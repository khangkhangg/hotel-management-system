-- +goose Up
-- What a hotel sells besides rooms: services, tours and products (spec 07). Selling one posts a folio line.
create table extra (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  name text not null check (btrim(name) <> ''),
  kind text not null check (kind in ('service', 'tour', 'product')),
  unit text not null check (unit in ('each', 'per_night', 'per_person', 'per_person_night')),
  price bigint not null check (price > 0),
  active boolean not null default true,
  created_at timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id),
  unique (property_id, name)
);
alter table extra enable row level security;
alter table extra force row level security;
create policy extra_isolation on extra using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on extra to app;

-- +goose Down
drop table extra;
