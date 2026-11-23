-- +goose Up
-- A promo code gives a guest a percent or a fixed amount off a stay booked on the hotel's own booking page.
create table promo_code (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  code            text not null check (code ~ '^[A-Z0-9_-]{3,20}$'),
  kind            text not null check (kind in ('percent', 'amount')),
  value           bigint not null check (value > 0),
  min_nights      integer not null default 1 check (min_nights >= 1),
  arrive_from     date,
  arrive_to       date,
  max_uses        integer check (max_uses is null or max_uses >= 1),
  used            integer not null default 0 check (used >= 0),
  active          boolean not null default true,
  created_at      timestamptz not null default now(),
  foreign key (property_id, organization_id) references property (id, organization_id),
  unique (property_id, code),
  check (kind <> 'percent' or value <= 100),
  check (arrive_from is null or arrive_to is null or arrive_from <= arrive_to)
);

alter table promo_code enable row level security;
alter table promo_code force row level security;
create policy promo_code_isolation on promo_code using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update, delete on promo_code to app;

-- +goose Down
drop table promo_code;
