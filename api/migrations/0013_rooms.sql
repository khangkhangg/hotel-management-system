-- +goose Up
-- Rooms, rates and inventory (spec 03). Money is an integer in the smallest unit; the currency is the property's.
create table room_type (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  name text not null check (btrim(name) <> ''),
  base_guests integer not null check (base_guests >= 1),
  max_guests integer not null check (max_guests >= base_guests),
  extra_adult_price bigint not null default 0 check (extra_adult_price >= 0),
  extra_child_price bigint not null default 0 check (extra_child_price >= 0),
  created_at timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id),
  unique (id, organization_id),
  unique (property_id, name)
);
create index room_type_org_idx on room_type (organization_id, property_id);

create table room (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  room_type_id uuid not null,
  number text not null check (btrim(number) <> ''),
  floor integer not null default 0,
  status text not null default 'clean' check (status in ('clean', 'dirty', 'inspect', 'out_of_order')),
  created_at timestamptz not null,
  foreign key (room_type_id, organization_id) references room_type (id, organization_id),
  unique (id, organization_id),
  unique (property_id, number)
);
create index room_type_idx on room (room_type_id);

create table rate_plan (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  room_type_id uuid not null,
  template_id uuid,
  name text not null check (btrim(name) <> ''),
  free_cancel_until_days integer check (free_cancel_until_days >= 0),
  free_cancel_until_time time,
  late_cancel_penalty text not null default 'none' check (late_cancel_penalty in ('none', 'first_night', 'percent', 'full')),
  late_cancel_percent integer check (late_cancel_percent between 1 and 100),
  meal_plan text not null default 'none',
  created_at timestamptz not null,
  foreign key (room_type_id, organization_id) references room_type (id, organization_id),
  unique (id, organization_id),
  unique (room_type_id, name)
);

create table rate (
  rate_plan_id uuid not null,
  date date not null,
  organization_id uuid not null references organization (id),
  price bigint not null check (price >= 0),
  min_nights integer not null default 1 check (min_nights >= 1),
  closed boolean not null default false,
  closed_to_arrival boolean not null default false,
  closed_to_departure boolean not null default false,
  suspicious boolean not null default false,
  primary key (rate_plan_id, date),
  foreign key (rate_plan_id, organization_id) references rate_plan (id, organization_id)
);

create table inventory (
  room_type_id uuid not null,
  date date not null,
  organization_id uuid not null references organization (id),
  total integer not null default 0 check (total >= 0),
  sold integer not null default 0 check (sold >= 0),
  blocked integer not null default 0 check (blocked >= 0),
  oversold integer not null default 0 check (oversold >= 0),
  primary key (room_type_id, date),
  foreign key (room_type_id, organization_id) references room_type (id, organization_id),
  check (sold + blocked <= total)
);

create table tax_rule (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  name text not null check (btrim(name) <> ''),
  percent numeric(5, 2) not null check (percent >= 0 and percent <= 100),
  inclusive boolean not null default false,
  applies_to text not null default 'all' check (applies_to in ('all', 'room', 'extra')),
  foreign key (property_id, organization_id) references property (id, organization_id)
);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['room_type', 'room', 'rate_plan', 'rate', 'inventory', 'tax_rule'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
                      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd

-- +goose Down
drop table tax_rule, inventory, rate, rate_plan, room, room_type;
