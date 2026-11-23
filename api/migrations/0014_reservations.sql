-- +goose Up
create extension if not exists btree_gist;

-- A guest belongs to the organization, so a chain's properties share one profile.
create table guest (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  name text not null check (btrim(name) <> ''),
  email text check (email = lower(btrim(email))),
  phone text not null default '',
  country text not null default '',
  locale text not null default 'en',
  notes text not null default '',
  created_at timestamptz not null,
  unique (id, organization_id)
);
create unique index guest_email_idx on guest (organization_id, email) where email is not null;

create table reservation (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  code text not null unique,
  guest_id uuid not null,
  status text not null default 'confirmed' check (status in ('pending', 'confirmed', 'checked_in', 'checked_out', 'cancelled', 'no_show')),
  source_channel text not null default 'direct',
  channel_booking_ref text,
  total bigint not null check (total >= 0),
  currency char(3) not null,
  overbooked boolean not null default false,
  created_at timestamptz not null,
  cancelled_at timestamptz,
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (guest_id, organization_id) references guest (id, organization_id),
  unique (id, organization_id)
);
create index reservation_property_idx on reservation (property_id, created_at desc);
create index reservation_guest_idx on reservation (guest_id);

create table reservation_room (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  reservation_id uuid not null,
  room_type_id uuid not null,
  room_id uuid,
  rate_plan_id uuid not null,
  stay daterange not null check (not isempty(stay) and lower_inc(stay) and not upper_inc(stay)),
  adults integer not null check (adults >= 1),
  children integer not null default 0 check (children >= 0),
  active boolean not null default true,
  foreign key (reservation_id, organization_id) references reservation (id, organization_id),
  foreign key (room_type_id, organization_id) references room_type (id, organization_id),
  foreign key (room_id, organization_id) references room (id, organization_id),
  foreign key (rate_plan_id, organization_id) references rate_plan (id, organization_id),
  unique (id, organization_id),
  -- One room cannot hold two stays that overlap. A stay includes its first night and not its departure date,
  -- so a guest leaving on the 25th and one arriving on the 25th do not overlap.
  exclude using gist (room_id with =, stay with &&) where (room_id is not null and active)
);
create index reservation_room_reservation_idx on reservation_room (reservation_id);
create index reservation_room_type_idx on reservation_room (room_type_id);
create index reservation_room_room_idx on reservation_room (room_id);

create table reservation_night (
  reservation_room_id uuid not null,
  date date not null,
  organization_id uuid not null references organization (id),
  price bigint not null check (price >= 0),
  primary key (reservation_room_id, date),
  foreign key (reservation_room_id, organization_id) references reservation_room (id, organization_id)
);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['guest', 'reservation', 'reservation_room', 'reservation_night'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
                      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd
grant delete on reservation_night to app; -- a stay cut short or moved loses the nights it no longer uses

-- +goose Down
drop table reservation_night, reservation_room, reservation, guest;
