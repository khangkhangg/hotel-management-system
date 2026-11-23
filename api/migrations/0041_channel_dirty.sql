-- +goose Up
-- A booking or a rate change marks the hotel in the same transaction, so a push can be late but cannot be lost (spec 04).
create table channel_dirty (
  organization_id uuid not null references organization (id),
  property_id     uuid primary key,
  marked_at       timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id)
);
alter table channel_dirty enable row level security;
create policy channel_dirty_isolation on channel_dirty using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update, delete on channel_dirty to app;

-- +goose StatementBegin
create function mark_channel_dirty(p_org uuid, p_property uuid) returns void language sql as $$
  insert into channel_dirty (organization_id, property_id, marked_at) values (p_org, p_property, clock_timestamp())
  on conflict (property_id) do update set marked_at = excluded.marked_at
$$;

create function inventory_marks_channel() returns trigger language plpgsql as $$
begin
  perform mark_channel_dirty(new.organization_id, (select t.property_id from room_type t where t.id = new.room_type_id));
  return null;
end $$;

create function rate_marks_channel() returns trigger language plpgsql as $$
begin
  perform mark_channel_dirty(new.organization_id, (select p.property_id from rate_plan p where p.id = new.rate_plan_id));
  return null;
end $$;

-- The sweeper has no organization, so it reads the marked hotels with a switched-on connection through this function.
create function dirty_channel_properties() returns table (organization_id uuid, property_id uuid, marked_at timestamptz)
language sql stable security definer set search_path = public, pg_temp as $$
  select d.organization_id, d.property_id, d.marked_at from channel_dirty d
  where exists (select 1 from channel_connection c where c.property_id = d.property_id and c.enabled)
$$;
-- +goose StatementEnd
revoke all on function dirty_channel_properties() from public;
grant execute on function dirty_channel_properties() to app;
alter table channel_dirty no force row level security;

create trigger inventory_marks_channel after insert or update of total, sold, blocked on inventory for each row execute function inventory_marks_channel();
create trigger rate_marks_channel after insert or update of price, closed, min_nights on rate for each row execute function rate_marks_channel();

-- +goose Down
drop trigger rate_marks_channel on rate;
drop trigger inventory_marks_channel on inventory;
drop function dirty_channel_properties();
drop function rate_marks_channel();
drop function inventory_marks_channel();
drop function mark_channel_dirty(uuid, uuid);
drop table channel_dirty;
