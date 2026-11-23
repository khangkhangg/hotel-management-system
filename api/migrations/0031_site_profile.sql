-- +goose Up
-- What a hotel shows on its guest site besides rooms (spec 05 and 08): text in each language, amenities, photos and brand.
create table property_text (
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  locale          text not null check (locale in ('en', 'zh-Hans', 'hi', 'es', 'fr', 'ar', 'bn', 'pt', 'ru', 'ur')),
  field           text not null check (field in ('tagline', 'description', 'policies')),
  body            text not null check (btrim(body) <> '' and char_length(body) <= 2000),
  primary key (property_id, locale, field),
  foreign key (property_id, organization_id) references property (id, organization_id)
);

create table property_amenity (
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  code            text not null check (code ~ '^[a-z0-9_]{2,30}$'),
  primary key (property_id, code),
  foreign key (property_id, organization_id) references property (id, organization_id)
);

create table property_photo (
  id              uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  kind            text not null check (kind in ('gallery', 'logo')),
  content_type    text not null check (content_type in ('image/jpeg', 'image/png', 'image/webp')),
  caption         text not null default '' check (char_length(caption) <= 120),
  data            bytea not null check (octet_length(data) <= 2097152),
  created_at      timestamptz not null default now(),
  foreign key (property_id, organization_id) references property (id, organization_id)
);
create unique index property_logo_one on property_photo (property_id) where kind = 'logo';
create index property_photo_order on property_photo (property_id, created_at);

create table property_brand (
  organization_id uuid not null references organization (id),
  property_id     uuid primary key,
  color           text not null check (color ~ '^#[0-9A-Fa-f]{6}$'),
  foreign key (property_id, organization_id) references property (id, organization_id)
);

alter table property_text enable row level security;
alter table property_text force row level security;
create policy property_text_isolation on property_text using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table property_amenity enable row level security;
alter table property_amenity force row level security;
create policy property_amenity_isolation on property_amenity using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table property_photo enable row level security;
alter table property_photo force row level security;
create policy property_photo_isolation on property_photo using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
alter table property_brand enable row level security;
alter table property_brand force row level security;
create policy property_brand_isolation on property_brand using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);

grant select, insert, update, delete on property_text, property_amenity, property_photo, property_brand to app;

-- +goose Down
drop table property_brand;
drop table property_photo;
drop table property_amenity;
drop table property_text;
