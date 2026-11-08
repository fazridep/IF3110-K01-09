-- Data uji migration besar (ketentuan pengujian spesifikasi). Bikin tabel
-- orders_large lalu mengukur satu UPDATE tanpa batching, yang harus lebih dari
-- 1 detik. Jalankan: make seed-large, atau ROWS=5000000 make seed-large.
\set ON_ERROR_STOP on
\timing on

DROP TABLE IF EXISTS orders_large;
CREATE TABLE orders_large (
    id         BIGINT      PRIMARY KEY,
    customer   TEXT        NOT NULL,
    amount     NUMERIC(12, 2) NOT NULL,
    status     TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

\echo 'isi data'
INSERT INTO orders_large
SELECT i,
       'customer-' || (i % 50000),
       (i % 100000) / 100.0,
       (ARRAY['new', 'paid', 'shipped', 'done'])[1 + i % 4],
       now() - (i % 86400) * interval '1 second'
FROM generate_series(1, :rows) AS i;
ANALYZE orders_large;

SELECT count(*) AS jumlah_row FROM orders_large;

\echo 'UPDATE seluruh row tanpa batching, durasinya yang dicatat'
BEGIN;
UPDATE orders_large SET amount = amount * 1.1;
ROLLBACK;
