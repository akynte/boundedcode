CREATE TABLE ledger_entries (
    id           BIGSERIAL PRIMARY KEY,
    payment_id   TEXT NOT NULL,
    account_id   TEXT NOT NULL,
    amount_cents BIGINT NOT NULL,
    currency     CHAR(3) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
