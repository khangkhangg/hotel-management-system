-- +goose Up
-- One channel connection per property (spec 04): either through a channel manager or straight to one OTA.
-- The secret is never stored here, only the name of the server setting that holds it.
create table channel_connection (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  mode text not null check (mode in ('channel_manager', 'direct')),
  provider text not null check (provider in ('cm_generic', 'agoda', 'booking_com', 'expedia', 'fake')),
  external_hotel_id text not null default '',
  base_url text not null default '',
  secret_setting text not null default '',
  enabled boolean not null default false,
  created_at timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id),
  unique (property_id),
  unique (id, organization_id)
);

create table channel_map (
  connection_id uuid not null,
  organization_id uuid not null references organization (id),
  room_type_id uuid not null,
  rate_plan_id uuid not null,
  external_room_id text not null check (btrim(external_room_id) <> ''),
  external_rate_id text not null default '',
  primary key (connection_id, rate_plan_id),
  foreign key (connection_id, organization_id) references channel_connection (id, organization_id) on delete cascade,
  foreign key (room_type_id, organization_id) references room_type (id, organization_id),
  foreign key (rate_plan_id, organization_id) references rate_plan (id, organization_id)
);

create table sync_log (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  connection_id uuid,
  direction text not null check (direction in ('push', 'pull', 'compare')),
  status text not null check (status in ('ok', 'error')),
  message text not null default '',
  created_at timestamptz not null
);
create index sync_log_property_idx on sync_log (property_id, created_at desc);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['channel_connection', 'channel_map', 'sync_log'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update, delete on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd

-- +goose Down
drop table sync_log;
drop table channel_map;
drop table channel_connection;
