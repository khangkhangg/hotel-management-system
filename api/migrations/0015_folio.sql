-- +goose Up
-- The folio: what a guest owes, what they paid, and refunds (spec 03 and 06). Amounts are integers in the smallest unit.
create table folio (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  property_id uuid not null,
  reservation_id uuid,
  status text not null default 'open' check (status in ('open', 'closed')),
  currency char(3) not null,
  created_at timestamptz not null,
  foreign key (property_id, organization_id) references property (id, organization_id),
  foreign key (reservation_id, organization_id) references reservation (id, organization_id),
  unique (id, organization_id),
  unique (reservation_id)
);

create table folio_line (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  folio_id uuid not null,
  kind text not null check (kind in ('room', 'extra', 'tax', 'adjustment')),
  description text not null check (btrim(description) <> ''),
  quantity integer not null default 1 check (quantity >= 1),
  unit_price bigint not null,
  tax_included bigint not null default 0 check (tax_included >= 0), -- the part of a room price that is tax; information only
  auto boolean not null default false, -- built from the reservation's nights and rebuilt when they change
  night date,
  posted_by uuid references app_user (id),
  created_at timestamptz not null,
  foreign key (folio_id, organization_id) references folio (id, organization_id)
);
create index folio_line_folio_idx on folio_line (folio_id);

create table payment (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  folio_id uuid not null,
  method text not null check (method in ('cash', 'card_terminal', 'bank_transfer', 'other')),
  amount bigint not null check (amount > 0),
  note text not null default '',
  status text not null default 'paid' check (status in ('paid', 'partly_refunded', 'refunded')),
  taken_by uuid references app_user (id),
  created_at timestamptz not null,
  foreign key (folio_id, organization_id) references folio (id, organization_id),
  unique (id, organization_id)
);
create index payment_folio_idx on payment (folio_id);

create table refund (
  id uuid primary key,
  organization_id uuid not null references organization (id),
  payment_id uuid not null,
  amount bigint not null check (amount > 0),
  reason text not null check (btrim(reason) <> ''),
  status text not null check (status in ('pending', 'done', 'rejected')),
  requested_by uuid references app_user (id),
  approved_by uuid references app_user (id),
  created_at timestamptz not null,
  decided_at timestamptz,
  foreign key (payment_id, organization_id) references payment (id, organization_id),
  unique (id, organization_id)
);
create index refund_payment_idx on refund (payment_id);

-- How much a role may refund without approval, per property. A role with no row uses the default in the code.
create table refund_limit (
  property_id uuid not null,
  organization_id uuid not null references organization (id),
  role text not null check (role in ('front_desk')),
  amount bigint not null check (amount >= 0),
  primary key (property_id, role),
  foreign key (property_id, organization_id) references property (id, organization_id)
);

-- +goose StatementBegin
do $$
declare t text;
begin
  foreach t in array array['folio', 'folio_line', 'payment', 'refund', 'refund_limit'] loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format($p$create policy %I on %I using (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)
                      with check (organization_id = nullif(current_setting('app.org_id', true), '')::uuid)$p$, t || '_isolation', t);
    execute format('grant select, insert, update on %I to app', t);
  end loop;
end $$;
-- +goose StatementEnd
grant delete on folio_line to app; -- automatic lines are rebuilt; payments and refunds are never deleted

-- +goose Down
drop table refund_limit, refund, payment, folio_line, folio;
