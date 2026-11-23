-- +goose Up
-- A guest opens their booking from an emailed link. Only a hash of the link's secret is kept.
create table manage_token (
  token_hash text primary key,
  organization_id uuid not null references organization (id),
  reservation_id uuid not null,
  expires_at timestamptz not null,
  foreign key (reservation_id, organization_id) references reservation (id, organization_id)
);
create index manage_token_reservation_idx on manage_token (reservation_id);
alter table manage_token enable row level security;
alter table manage_token force row level security;
create policy manage_token_isolation on manage_token
  using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, delete on manage_token to app;

alter table refund add column provider_ref text;

-- +goose Down
alter table refund drop column provider_ref;
drop table manage_token;
