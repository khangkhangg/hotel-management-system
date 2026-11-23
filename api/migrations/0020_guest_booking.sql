-- +goose Up
-- Guest booking: payment types the owner turns on, holds on pending bookings, and card payments recorded once per provider event.
create table payment_type (
  property_id uuid not null,
  kind text not null check (kind in ('hotel', 'bank_transfer', 'card')),
  organization_id uuid not null references organization (id),
  enabled boolean not null default false,
  hold_hours int not null default 48 check (hold_hours between 1 and 336),
  bank_details text not null default '' check (length(bank_details) <= 1000),
  primary key (property_id, kind),
  foreign key (property_id, organization_id) references property (id, organization_id)
);
alter table payment_type enable row level security;
alter table payment_type force row level security;
create policy payment_type_isolation on payment_type
  using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update, delete on payment_type to app;

alter table reservation add column payment_kind text check (payment_kind in ('hotel', 'bank_transfer', 'card'));
alter table reservation add column hold_until timestamptz;
alter table reservation add column paid_late boolean not null default false;

alter table payment drop constraint payment_method_check;
alter table payment add constraint payment_method_check check (method in ('cash', 'card_terminal', 'bank_transfer', 'other', 'card_online'));
alter table payment add column provider text;
alter table payment add column provider_ref text;

create table payment_event (
  provider text not null,
  event_id text not null,
  organization_id uuid not null references organization (id),
  received_at timestamptz not null,
  primary key (provider, event_id)
);
alter table payment_event enable row level security;
alter table payment_event force row level security;
create policy payment_event_isolation on payment_event
  using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert on payment_event to app;

-- A guest has no session, so the public address is turned into a property here. The property table is readable by its owner role.
-- +goose StatementBegin
create function property_by_slug(p_slug text) returns table (organization_id uuid, property_id uuid)
language sql stable security definer set search_path = public, pg_temp as $$
  select organization_id, id from property where slug = p_slug
$$;
-- +goose StatementEnd
revoke all on function property_by_slug(text) from public;
grant execute on function property_by_slug(text) to app;

-- +goose Down
drop function property_by_slug(text);
drop table payment_event;
alter table payment drop column provider_ref;
alter table payment drop column provider;
delete from payment where method = 'card_online';
alter table payment drop constraint payment_method_check;
alter table payment add constraint payment_method_check check (method in ('cash', 'card_terminal', 'bank_transfer', 'other'));
alter table reservation drop column paid_late;
alter table reservation drop column hold_until;
alter table reservation drop column payment_kind;
drop table payment_type;
