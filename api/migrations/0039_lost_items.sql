-- +goose Up
-- Things guests leave behind, kept until they are returned or thrown away.
create table lost_item (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  description     text not null check (btrim(description) <> '' and char_length(description) <= 300),
  location        text not null default '' check (char_length(location) <= 200),
  found_on        date not null,
  status          text not null default 'stored' check (status in ('stored', 'returned', 'discarded')),
  owner_name      text not null default '' check (char_length(owner_name) <= 200),
  owner_contact   text not null default '' check (char_length(owner_contact) <= 200),
  note            text not null default '' check (char_length(note) <= 1000),
  created_at      timestamptz not null,
  updated_at      timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id)
);
create index lost_item_property on lost_item (property_id, status, found_on desc);

alter table lost_item enable row level security;
alter table lost_item force row level security;
create policy lost_item_isolation on lost_item using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on lost_item to app;

-- +goose Down
drop table lost_item;
