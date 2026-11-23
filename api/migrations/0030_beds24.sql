-- +goose Up
alter table channel_connection drop constraint channel_connection_provider_check;
alter table channel_connection add constraint channel_connection_provider_check check (provider in ('cm_generic', 'channex', 'beds24', 'agoda', 'booking_com', 'expedia', 'google', 'fake'));

-- +goose Down
alter table channel_connection drop constraint channel_connection_provider_check;
alter table channel_connection add constraint channel_connection_provider_check check (provider in ('cm_generic', 'channex', 'agoda', 'booking_com', 'expedia', 'google', 'fake'));
