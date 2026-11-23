-- +goose Up
-- The same key in another organization or at another property is a different notification.
drop index notification_open_key;
create unique index notification_open_key on notification (organization_id, property_id, user_id, key) where resolved_at is null;

-- +goose Down
drop index notification_open_key;
create unique index notification_open_key on notification (user_id, key) where resolved_at is null;
