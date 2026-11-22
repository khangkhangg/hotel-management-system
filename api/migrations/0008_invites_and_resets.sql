-- +goose Up
-- Invites, password resets, and the rule that "owner" and "organization-wide" mean the same thing (spec 01, 02).

-- An owner works across the whole organization (no property); every other role belongs to one property.
alter table membership add constraint membership_owner_is_org_wide check ((role = 'owner') = (property_id is null));

create table invite (
    id              uuid primary key,
    organization_id uuid not null references organization (id),
    property_id     uuid,
    email           text not null check (email = lower(btrim(email))),
    role            text not null check (role in ('owner', 'manager', 'front_desk', 'revenue', 'housekeeping')),
    token_hash      bytea not null unique,
    invited_by      uuid references app_user (id),
    created_at      timestamptz not null,
    expires_at      timestamptz not null,
    accepted_at     timestamptz,
    foreign key (property_id, organization_id) references property (id, organization_id),
    check ((role = 'owner') = (property_id is null))
);
create index invite_organization_idx on invite (organization_id);

alter table invite enable row level security;
alter table invite force row level security;
create policy invite_isolation on invite
    using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
    with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- The person following an invite link has no session and no organization yet. The server sets
-- app.invite_hash to the hash of the token it was given, so only that one invite becomes visible.
create policy invite_by_token on invite
    using (token_hash = decode(nullif(current_setting('app.invite_hash', true), ''), 'hex'));

grant select, insert, update, delete on invite to app;

-- Password reset links: single use, 30 minutes. Only a hash of each token is stored.
create table password_reset (
    id         uuid primary key,
    user_id    uuid not null references app_user (id),
    token_hash bytea not null unique,
    created_at timestamptz not null,
    expires_at timestamptz not null,
    used_at    timestamptz
);
create index password_reset_user_idx on password_reset (user_id, created_at desc);
grant select, insert, update on password_reset to app;

-- +goose Down
drop table password_reset;
drop table invite;
alter table membership drop constraint membership_owner_is_org_wide;
