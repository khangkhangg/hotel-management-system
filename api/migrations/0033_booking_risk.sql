-- +goose Up
-- The fraud risk of a guest booking: a score, the reasons for it, and what staff decided. Addresses and emails are kept
-- only as short hashes, so the table can count repeats without holding personal data.
create table booking_risk (
  reservation_id  uuid primary key,
  organization_id uuid not null references organization (id),
  property_id     uuid not null,
  score           integer not null check (score >= 0),
  level           text not null check (level in ('low', 'medium', 'high')),
  reasons         jsonb not null default '[]',
  status          text not null check (status in ('clear', 'review', 'approved', 'declined')),
  ip_hash         text not null,
  email_hash      text not null,
  created_at      timestamptz not null,
  decided_by      uuid,
  decided_at      timestamptz,
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (reservation_id, organization_id) references reservation (id, organization_id)
);
create index booking_risk_ip on booking_risk (property_id, ip_hash, created_at);
create index booking_risk_email on booking_risk (property_id, email_hash, created_at);
create index booking_risk_review on booking_risk (property_id) where status = 'review';

alter table booking_risk enable row level security;
alter table booking_risk force row level security;
create policy booking_risk_isolation on booking_risk using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
  with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid);
grant select, insert, update on booking_risk to app;

-- +goose Down
drop table booking_risk;
