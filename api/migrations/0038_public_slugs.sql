-- +goose Up
-- The function runs as the table owner, which row level security skips only when the table is not forced.
alter table room_type no force row level security;
-- +goose StatementBegin
create function public_property_slugs() returns table (slug text)
language sql stable security definer set search_path = public, pg_temp as $$
select p.slug from property p where exists (select 1 from room_type t where t.property_id = p.id) order by p.slug
$$;
-- +goose StatementEnd
revoke all on function public_property_slugs() from public;
grant execute on function public_property_slugs() to app;

-- +goose Down
alter table room_type force row level security;
drop function public_property_slugs();
