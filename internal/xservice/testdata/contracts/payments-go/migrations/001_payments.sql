-- Payments are owned by this service.
CREATE TABLE IF NOT EXISTS payments (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL,
    amount_cents BIGINT NOT NULL
);
CREATE INDEX payments_account ON payments (account_id);
