-- +goose Up
-- Two small promises to guests: ask a guest who may not come to reconfirm, and tell a guest on the waiting list when a room frees up.
create table reconfirm (
  reservation_id  uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  token_hash      bytea not null unique,
  sent_at         timestamptz not null,
  confirmed_at    timestamptz,
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (reservation_id, organization_id) references reservation (id, organization_id)
);

create table waitlist (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  name            text not null check (btrim(name) <> '' and char_length(name) <= 200),
  email           text not null check (email = lower(btrim(email)) and char_length(email) <= 200),
  locale          text not null default 'en',
  arrive          date not null,
  depart          date not null,
  adults          integer not null check (adults between 1 and 20),
  created_at      timestamptz not null,
  notified_at     timestamptz,
  foreign key (property_id, organization_id) references property (id, organization_id),
  check (depart > arrive),
  unique (property_id, email, arrive, depart)
);
create index waitlist_open on waitlist (property_id) where notified_at is null;

alter table reconfirm enable row level security;
alter table reconfirm force row level security;
create policy reconfirm_isolation on reconfirm using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table waitlist enable row level security;
alter table waitlist force row level security;
create policy waitlist_isolation on waitlist using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update, delete on reconfirm, waitlist to app;

-- +goose Down
drop table waitlist;
drop table reconfirm;
