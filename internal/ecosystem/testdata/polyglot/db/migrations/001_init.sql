CREATE TABLE IF NOT EXISTS orders (
    id BIGINT PRIMARY KEY,
    total_cents BIGINT NOT NULL
);

CREATE INDEX orders_total_idx ON orders (total_cents);

CREATE OR REPLACE VIEW big_orders AS SELECT * FROM orders WHERE total_cents > 10000;
