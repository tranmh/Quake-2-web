BEGIN;
SELECT plan(24);

\ir seeds/seed.sql

-- users: email unique case-insensitively (B11 email_taken), defaults
SELECT throws_ok(
    $$INSERT INTO users (email, password_hash, display_name) VALUES ('Bob@Example.Test', 'x', 'dup')$$,
    '23505', NULL, 'mixed-case duplicate email is rejected');
SELECT lives_ok(
    $$INSERT INTO users (id, email, password_hash, display_name) OVERRIDING SYSTEM VALUE
      VALUES (900010, 'carol@example.test', 'x', 'Carol')$$,
    'a new distinct email is accepted');
SELECT is((SELECT is_admin FROM users WHERE id = 900010), false, 'is_admin defaults to false');
SELECT ok((SELECT created_at IS NOT NULL FROM users WHERE id = 900010), 'created_at defaults to now()');
SELECT throws_ok(
    $$INSERT INTO users (email, password_hash, display_name) VALUES (NULL, 'x', 'n')$$,
    '23502', NULL, 'NULL email is rejected');

-- blobs: sha256 must be 64 lower-case hex chars
SELECT throws_ok(
    $$INSERT INTO blobs (sha256, size, content_type) VALUES (repeat('A', 64), 1, 'x')$$,
    '23514', NULL, 'upper-case blob hash is rejected');
SELECT throws_ok(
    $$INSERT INTO blobs (sha256, size, content_type) VALUES (repeat('a', 63), 1, 'x')$$,
    '23514', NULL, '63-char blob hash is rejected');
SELECT throws_ok(
    $$INSERT INTO blobs (sha256, size, content_type) VALUES (repeat('a', 65), 1, 'x')$$,
    '23514', NULL, '65-char blob hash is rejected');
SELECT lives_ok(
    $$INSERT INTO blobs (sha256, size, content_type) VALUES (repeat('f', 64), 1, 'x')$$,
    'a 64-char lower-case hex hash is accepted');

-- paks: status domain, unique content hash, defaults
SELECT throws_ok(
    $$INSERT INTO paks (sha256, name, size, status) VALUES (repeat('6', 64), 'x.pak', 1, 'done')$$,
    '23514', NULL, 'unknown pak status is rejected');
SELECT throws_ok(
    $$INSERT INTO paks (sha256, name, size) VALUES (repeat('3', 64), 'again.pak', 1)$$,
    '23505', NULL, 'the same pak content cannot be stored twice');
INSERT INTO paks (sha256, name, size) VALUES (repeat('7', 64), 'n.pak', 1);
SELECT is(
    (SELECT status || '/' || public::text FROM paks WHERE sha256 = repeat('7', 64)),
    'pending/false', 'a new pak defaults to pending and private');
SELECT is_empty(
    $$INSERT INTO paks (sha256, name, size) VALUES (repeat('3', 64), 'again.pak', 1)
      ON CONFLICT (sha256) DO NOTHING RETURNING id$$,
    'CreatePak upsert returns no row for known content (B18 dedup)');

-- jobs: status domain and default
SELECT throws_ok(
    $$INSERT INTO jobs (kind, status) VALUES ('ingest', 'pending')$$,
    '23514', NULL, 'unknown job status is rejected');
SELECT results_eq(
    $$INSERT INTO jobs (kind) VALUES ('ingest') RETURNING status$$,
    $$VALUES ('queued'::text)$$, 'a new job defaults to queued');

-- bans: a ban needs a user or an ip
SELECT throws_ok(
    $$INSERT INTO bans (user_id, ip) VALUES (NULL, '')$$,
    '23514', NULL, 'a ban with neither user nor ip is rejected');

-- bot_spend: non-negative
SELECT throws_ok(
    $$INSERT INTO bot_spend (day, usd) VALUES ('2026-02-02', -0.01)$$,
    '23514', NULL, 'negative bot spend is rejected');
SELECT lives_ok(
    $$INSERT INTO bot_spend (day, usd) VALUES ('2026-02-02', 0)$$,
    'zero bot spend is accepted');

-- pakset_paks: a pak in a pakset cannot be deleted
SELECT throws_ok(
    $$DELETE FROM paks WHERE id = 900102$$,
    '23503', NULL, 'a pak referenced by a pakset cannot be deleted (RESTRICT)');

-- cascades on user deletion
DELETE FROM users WHERE id = 900002;
SELECT is((SELECT count(*)::int FROM sessions WHERE user_id = 900002), 0, 'deleting a user deletes the sessions');
SELECT is((SELECT count(*)::int FROM saves WHERE user_id = 900002), 0, 'deleting a user deletes the saves');
SELECT is((SELECT count(*)::int FROM paksets WHERE id = 'alice-mod'), 0, 'deleting a user deletes owned paksets');
SELECT ok((SELECT owner_id IS NULL FROM games WHERE id = 'g-active'), 'deleting a user keeps the game with owner NULL');
SELECT ok((SELECT user_id IS NULL FROM jobs WHERE id = 900202), 'deleting a user keeps the job with user NULL');

SELECT * FROM finish();
ROLLBACK;
