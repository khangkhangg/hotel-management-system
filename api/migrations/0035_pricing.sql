-- +goose Up
-- Price suggestions: the lowest and highest price a room type may have, what competitors charge on a night, and which
-- nights staff changed lately, so a night is not suggested again at once.
create table price_limit (
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  room_type_id    uuid primary key,
  floor_price     bigint check (floor_price is null or floor_price >= 1),
  ceiling_price   bigint check (ceiling_price is null or ceiling_price >= 1),
  foreign key (property_id, organization_id) references property (id, organization_id),
  check (floor_price is null or ceiling_price is null or floor_price <= ceiling_price)
);

create table competitor_rate (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  date            date not null,
  name            text not null check (btrim(name) <> '' and char_length(name) <= 60),
  price           bigint not null check (price >= 1),
  created_at      timestamptz not null default now(),
  foreign key (property_id, organization_id) references property (id, organization_id),
  unique (property_id, date, name)
);

create table price_review (
  organization_id uuid not null references organization (id),
  rate_plan_id    uuid not null,
  date            date not null,
  reviewed_at     timestamptz not null,
  primary key (rate_plan_id, date)
);

alter table price_limit enable row level security;
alter table price_limit force row level security;
create policy price_limit_isolation on price_limit using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table competitor_rate enable row level security;
alter table competitor_rate force row level security;
create policy competitor_rate_isolation on competitor_rate using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table price_review enable row level security;
alter table price_review force row level security;
create policy price_review_isolation on price_review using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update, delete on price_limit, competitor_rate, price_review to app;

-- +goose Down
drop table price_review;
drop table competitor_rate;
drop table price_limit;
