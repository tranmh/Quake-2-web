BEGIN;
SELECT plan(8);

\ir seeds/seed.sql

SELECT is((SELECT count(*)::int FROM users WHERE id BETWEEN 900001 AND 900003), 3, 'seed inserts three users');
SELECT ok((SELECT is_admin FROM users WHERE id = 900001), 'seed admin user is an admin');
SELECT is((SELECT status FROM paks WHERE id = 900101), 'ready', 'seed pak0 is ready');
SELECT is((SELECT count(*)::int FROM pak_assets WHERE pak_id = 900101), 3, 'seed pak0 makes three assets available');
SELECT is((SELECT count(*)::int FROM pakset_paks WHERE pakset_id = 'alice-mod'), 2, 'seed alice-mod pakset has two paks');
SELECT is((SELECT count(*)::int FROM games WHERE ended_at IS NULL AND id LIKE 'g-%'), 1, 'seed has one active game');
SELECT is((SELECT usd FROM bot_spend WHERE day = '2026-02-01'), 1.25::double precision, 'seed bot spend for 2026-02-01');
SELECT throws_ok(
    $$INSERT INTO users (email, password_hash, display_name) VALUES ('ALICE@example.test', 'x', 'dup')$$,
    '23505', NULL, 'user email is unique case-insensitively against the seed');

SELECT * FROM finish();
ROLLBACK;
