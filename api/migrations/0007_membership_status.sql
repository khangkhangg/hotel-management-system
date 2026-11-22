-- +goose Up
-- Deactivating a person ends their access to one organization, not to every organization they work in.
-- A membership is therefore deactivated, never deleted, and sign-in only offers organizations where it is active.

alter table membership add column status text not null default 'active' check (status in ('active', 'deactivated'));

drop policy organization_of_member on organization;
create policy organization_of_member on organization for select
    using (id in (select organization_id from membership
                  where status = 'active' and user_id = nullif(current_setting('app.user_id', true), '')::uuid));

-- +goose Down
drop policy organization_of_member on organization;
create policy organization_of_member on organization for select
    using (id in (select organization_id from membership
                  where user_id = nullif(current_setting('app.user_id', true), '')::uuid));
alter table membership drop column status;
