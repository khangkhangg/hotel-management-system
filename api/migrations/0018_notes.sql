-- +goose Up
-- Team notes on bookings (spec 11). Notes are internal: nothing here is ever sent to a guest or a channel.
create table note (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  reservation_id uuid,
  room_id uuid,
  parent_id uuid,
  author_id uuid not null references app_user (id),
  body text not null check (btrim(body) <> '' and char_length(body) <= 2000),
  pinned boolean not null default false,
  for_housekeeping boolean not null default false, -- tagged housekeeping, or a housekeeper's reply: shown on the room's task
  created_at timestamptz not null,
  edited_at timestamptz,
  removed_at timestamptz,
  removed_by uuid references app_user (id),
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (reservation_id, organization_id) references reservation (id, organization_id),
  foreign key (room_id, organization_id) references room (id, organization_id),
  unique (id, organization_id)
);
create index note_reservation_idx on note (reservation_id, created_at);
create index note_room_idx on note (room_id, created_at) where for_housekeeping;

create table note_mention (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  note_id uuid not null,
  user_id uuid references app_user (id),
  role text check (role in ('manager', 'front_desk', 'revenue', 'housekeeping')),
  done_at timestamptz,
  check ((user_id is null) <> (role is null)),
  foreign key (note_id, organization_id) references note (id, organization_id)
);
create index note_mention_note_idx on note_mention (note_id);

create table note_read (
  note_id uuid not null,
  user_id uuid not null references app_user (id),
  organization_id uuid not null references organization (id),
  read_at timestamptz not null,
  primary key (note_id, user_id),
  foreign key (note_id, organization_id) references note (id, organization_id)
);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['note', 'note_mention', 'note_read'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
                      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd

-- +goose Down
drop table note_read, note_mention, note;
