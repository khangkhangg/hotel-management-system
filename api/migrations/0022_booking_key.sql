-- +goose Up
-- A website booking carries a key the guest's browser makes once, so a double click or a retry finds the booking it already made.
create unique index reservation_website_key_idx on reservation (organization_id, channel_booking_ref) where source_channel = 'website' and channel_booking_ref is not null;

-- +goose Down
drop index reservation_website_key_idx;
