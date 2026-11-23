-- +goose Up
-- Guest reviews after a stay, and the messages between a guest and the hotel about one booking.
create table review (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  reservation_id  uuid not null unique,
  rating          smallint not null check (rating between 1 and 5),
  comment         text not null default '' check (char_length(comment) <= 2000),
  name            text not null check (btrim(name) <> '' and char_length(name) <= 100),
  status          text not null default 'published' check (status in ('published', 'hidden')),
  reply           text not null default '' check (char_length(reply) <= 1000),
  created_at      timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (reservation_id, organization_id) references reservation (id, organization_id)
);
create index review_public on review (property_id, created_at desc) where status = 'published';

create table guest_message (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  reservation_id  uuid not null,
  sender          text not null check (sender in ('guest', 'staff')),
  body            text not null check (btrim(body) <> '' and char_length(body) <= 2000),
  created_at      timestamptz not null,
  read_at         timestamptz,
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (reservation_id, organization_id) references reservation (id, organization_id)
);
create index guest_message_thread on guest_message (reservation_id, created_at);
create index guest_message_unread on guest_message (property_id) where sender = 'guest' and read_at is null;

alter table review enable row level security;
alter table review force row level security;
create policy review_isolation on review using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table guest_message enable row level security;
alter table guest_message force row level security;
create policy guest_message_isolation on guest_message using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on review, guest_message to app;

-- +goose Down
drop table guest_message;
drop table review;
