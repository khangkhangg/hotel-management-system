-- +goose Up
-- Tenancy: organizations, properties, people and their roles (spec 03).
-- Roles: "migrator" owns the tables, "app" is what the API connects as and has only the rights below.

create table organization (
    id         uuid primary key,
    name       text not null check (btrim(name) <> ''),
    created_at timestamptz not null default now()
);

create table property (
    id              uuid primary key,
    organization_id uuid not null references organization (id),
    name            text not null check (btrim(name) <> ''),
    slug            text not null unique check (slug ~ '^[a-z0-9][a-z0-9-]*$'),
    time_zone       text not null,
    currency        char(3) not null check (currency ~ '^[A-Z]{3}$'),
    default_locale  text not null,
    address         text not null default '',
    created_at      timestamptz not null default now(),
    unique (id, organization_id)
);
create index property_organization_idx on property (organization_id);

-- A person. Belongs to no organization; memberships say where the person works.
create table app_user (
    id              uuid primary key,
    email           text not null unique check (email = lower(btrim(email))),
    name            text not null,
    password_hash   text,
    google_subject  text unique,
    status          text not null default 'active' check (status in ('invited', 'active', 'deactivated')),
    last_sign_in_at timestamptz,
    created_at      timestamptz not null default now()
);

create table membership (
    id              uuid primary key,
    organization_id uuid not null references organization (id),
    user_id         uuid not null references app_user (id),
    property_id     uuid,
    role            text not null check (role in ('owner', 'manager', 'front_desk', 'revenue', 'housekeeping')),
    created_at      timestamptz not null default now(),
    foreign key (property_id, organization_id) references property (id, organization_id),
    unique (user_id, property_id)
);
-- unique (user_id, property_id) does not stop two organization-wide rows, because NULLs differ.
create unique index membership_organization_wide_idx on membership (user_id, organization_id) where property_id is null;
create index membership_organization_idx on membership (organization_id);

-- Row-level security: a request sees only the organization named in app.org_id.
-- With no setting, current_setting returns '' and the policy matches nothing.
alter table organization enable row level security;
alter table organization force row level security;
create policy organization_isolation on organization
    using (id = nullif(current_setting('app.org_id', true), '')::uuid)
    with check (id = nullif(current_setting('app.org_id', true), '')::uuid);

alter table property enable row level security;
alter table property force row level security;
create policy property_isolation on property
    using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
    with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);

alter table membership enable row level security;
alter table membership force row level security;
create policy membership_isolation on membership
    using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
    with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);

-- No delete rights: users are deactivated, never removed.
grant select, insert, update on organization, property, membership, app_user to app;

-- +goose Down
drop table membership;
drop table app_user;
drop table property;
drop table organization;
