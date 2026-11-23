-- +goose Up
create table housekeeping_task (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  room_id uuid not null,
  date date not null,
  kind text not null default 'checkout_clean' check (kind in ('checkout_clean', 'stayover', 'inspection', 'other')),
  status text not null default 'todo' check (status in ('todo', 'in_progress', 'done')),
  assigned_to uuid references app_user (id),
  created_at timestamptz not null,
  done_at timestamptz,
  foreign key (room_id, organization_id) references room (id, organization_id),
  unique (id, organization_id)
);
-- One open task of a kind per room and day.
create unique index housekeeping_open_idx on housekeeping_task (room_id, date, kind) where status <> 'done';
create index housekeeping_property_idx on housekeeping_task (property_id, date);

create table maintenance_ticket (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  room_id uuid not null,
  description text not null check (btrim(description) <> ''),
  status text not null default 'open' check (status in ('open', 'in_progress', 'done')),
  blocks_from date,
  blocks_to date,
  resolution text not null default '',
  created_by uuid references app_user (id),
  created_at timestamptz not null,
  closed_at timestamptz,
  check ((blocks_from is null) = (blocks_to is null) and (blocks_from is null or blocks_to > blocks_from)),
  foreign key (room_id, organization_id) references room (id, organization_id),
  unique (id, organization_id)
);
create index maintenance_room_idx on maintenance_ticket (room_id, created_at desc);

-- The nights a ticket actually took out of sale, so closing it gives back exactly those.
create table ticket_block (
  ticket_id uuid not null,
  date date not null,
  organization_id uuid not null references organization (id),
  primary key (ticket_id, date),
  foreign key (ticket_id, organization_id) references maintenance_ticket (id, organization_id)
);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['housekeeping_task', 'maintenance_ticket', 'ticket_block'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
                      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd

-- +goose Down
drop table ticket_block, maintenance_ticket, housekeeping_task;
