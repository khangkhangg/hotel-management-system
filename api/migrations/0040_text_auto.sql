-- +goose Up
-- A text that a translation service wrote stays a draft until a person edits it.
alter table property_text add column auto boolean not null default false;

-- +goose Down
alter table property_text drop column auto;
