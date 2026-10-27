BEGIN;
SELECT plan(16);

\ir ../seeds/seed.sql

-- The queries of internal/db/postgres.go, run against the seed data.

-- AssetAccess (B21): public ready pak serves to all, owners only otherwise.
CREATE FUNCTION pg_temp.asset_ok(sha text, uid bigint) RETURNS boolean LANGUAGE sql AS $$
    SELECT EXISTS (SELECT 1 FROM pak_assets a JOIN paks k ON k.id = a.pak_id
                    WHERE a.sha256 = b.sha256 AND k.status = 'ready'
                      AND (k.public OR EXISTS (SELECT 1 FROM pak_owners o
                                                WHERE o.pak_id = k.id AND o.user_id = uid)))
      FROM blobs b WHERE b.sha256 = sha
$$;

SELECT is(pg_temp.asset_ok(repeat('1', 64), 0), true, 'asset of a public ready pak is open to anonymous');
SELECT is(pg_temp.asset_ok(repeat('2', 64), 900003), true, 'asset of a public ready pak is open to a non-owner');
SELECT is(pg_temp.asset_ok(repeat('9', 64), 900001), NULL, 'unknown hash finds no blob (404)');
SELECT is(pg_temp.asset_ok(repeat('3', 64), 0), false, 'blob not listed as an asset of any pak is not entitled');

-- a private ready pak with an asset only its owner may load
INSERT INTO blobs (sha256, size, content_type) VALUES (repeat('8', 64), 10, 'image/png');
UPDATE paks SET status = 'ready' WHERE id = 900102;
INSERT INTO pak_assets (pak_id, sha256, role) VALUES (900102, repeat('8', 64), 'png');
SELECT is(pg_temp.asset_ok(repeat('8', 64), 900002), true, 'owner may load an asset of a private pak');
SELECT is(pg_temp.asset_ok(repeat('8', 64), 900003), false, 'non-owner may not load an asset of a private pak');
SELECT is(pg_temp.asset_ok(repeat('8', 64), 0), false, 'anonymous may not load an asset of a private pak');
UPDATE paks SET status = 'ingesting' WHERE id = 900102;
SELECT is(pg_temp.asset_ok(repeat('8', 64), 900002), false, 'asset of a pak that is not ready is not served');

-- ListPaks (B20): own and public paks
SELECT results_eq(
    $$SELECT k.id FROM paks k
       WHERE k.public OR EXISTS (SELECT 1 FROM pak_owners o WHERE o.pak_id = k.id AND o.user_id = 900002)
       ORDER BY k.id$$,
    ARRAY[900101::bigint, 900102::bigint], 'Alice lists the public pak and her own private pak');
SELECT results_eq(
    $$SELECT k.id FROM paks k
       WHERE k.public OR EXISTS (SELECT 1 FROM pak_owners o WHERE o.pak_id = k.id AND o.user_id = 900003)
       ORDER BY k.id$$,
    ARRAY[900101::bigint], 'Bob lists only the public pak');

-- ActiveBan (B13 banned)
CREATE FUNCTION pg_temp.ban_id(uid bigint, addr text, at timestamptz) RETURNS bigint LANGUAGE sql AS $$
    SELECT id FROM bans
     WHERE ((uid <> 0 AND user_id = uid) OR (addr <> '' AND ip = addr))
       AND (expires_at IS NULL OR expires_at > at)
     ORDER BY id LIMIT 1
$$;
SELECT is(pg_temp.ban_id(900003, '', '2026-02-15T00:00:00Z'), 900301::bigint, 'user ban is active before it expires');
SELECT is(pg_temp.ban_id(900003, '', '2026-03-01T00:00:00Z'), NULL::bigint, 'user ban is over at its expiry instant');
SELECT is(pg_temp.ban_id(0, '198.51.100.7', '2030-01-01T00:00:00Z'), 900302::bigint, 'permanent ip ban never expires');
SELECT is(pg_temp.ban_id(900002, '', '2026-02-15T00:00:00Z'), NULL::bigint, 'an unbanned user has no active ban');

-- AddBotSpend: accumulates per day
SELECT results_eq(
    $$INSERT INTO bot_spend (day, usd) VALUES ('2026-02-01'::date, 0.5)
      ON CONFLICT (day) DO UPDATE SET usd = bot_spend.usd + EXCLUDED.usd, updated_at = now()
      RETURNING usd$$,
    $$VALUES (1.75::double precision)$$, 'bot spend adds to the existing day total');
SELECT results_eq(
    $$INSERT INTO bot_spend (day, usd) VALUES ('2026-02-03'::date, 0.25)
      ON CONFLICT (day) DO UPDATE SET usd = bot_spend.usd + EXCLUDED.usd, updated_at = now()
      RETURNING usd$$,
    $$VALUES (0.25::double precision)$$, 'bot spend starts a new day at the given amount');

SELECT * FROM finish();
ROLLBACK;
