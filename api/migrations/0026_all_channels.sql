-- +goose Up
-- The background sync needs every organization's enabled connections, and row-level security shows one organization at a time.
alter table channel_connection no force row level security;
-- +goose StatementBegin
create function all_channel_connections() returns table (organization_id uuid, property_id uuid)
language sql stable security definer set search_path = public, pg_temp as $$
  select organization_id, property_id from channel_connection where enabled
$$;
-- +goose StatementEnd
revoke all on function all_channel_connections() from public;
grant execute on function all_channel_connections() to app;

-- +goose Down
drop function all_channel_connections();
alter table channel_connection force row level security;
