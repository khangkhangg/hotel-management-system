-- +goose Up
-- Sign-in happens before a person has chosen an organization, but a person may belong to several.
-- These read-only policies let a request that names the person (app.user_id) see that person's own
-- memberships and the organizations they belong to, across organizations. They grant no write access
-- and no access to properties or anything else.

create policy membership_own on membership for select
    using (user_id = nullif(current_setting('app.user_id', true), '')::uuid);

create policy organization_of_member on organization for select
    using (id in (select organization_id from membership
                  where user_id = nullif(current_setting('app.user_id', true), '')::uuid));

-- +goose Down
drop policy organization_of_member on organization;
drop policy membership_own on membership;
