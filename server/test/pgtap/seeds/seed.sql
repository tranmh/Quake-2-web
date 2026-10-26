-- Shared seed data for the pgTAP tests (include inside a test's transaction
-- with \ir seeds/seed.sql; everything rolls back with the test).
-- Fixed ids (900001..) and fixed timestamps keep the data deterministic.

INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at)
OVERRIDING SYSTEM VALUE VALUES
    (900001, 'admin@example.test', '$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA', 'Admin',  true,  '2026-01-01T00:00:00Z'),
    (900002, 'alice@example.test', '$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA', 'Alice',  false, '2026-01-02T00:00:00Z'),
    (900003, 'bob@example.test',   '$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA', 'Bob',    false, '2026-01-03T00:00:00Z');

INSERT INTO sessions (id, user_id, created_at, expires_at, user_agent, ip) VALUES
    (repeat('a', 64), 900002, '2026-02-01T00:00:00Z', '2026-03-01T00:00:00Z', 'test-agent', '192.0.2.10'),
    (repeat('b', 64), 900003, '2025-01-01T00:00:00Z', '2025-02-01T00:00:00Z', 'test-agent', '192.0.2.11');

INSERT INTO blobs (sha256, size, content_type, created_at) VALUES
    (repeat('1', 64), 1024, 'application/octet-stream', '2026-01-05T00:00:00Z'),
    (repeat('2', 64), 2048, 'image/png',                '2026-01-05T00:00:00Z'),
    (repeat('3', 64), 4096, 'application/x-quake-pak',  '2026-01-05T00:00:00Z'),
    (repeat('4', 64), 512,  'application/json',         '2026-01-05T00:00:00Z');

INSERT INTO paks (id, sha256, name, size, checksum, num_files, public, status, manifest_sha256, created_at, ingested_at)
OVERRIDING SYSTEM VALUE VALUES
    (900101, repeat('3', 64), 'pak0.pak', 4096, 12345, 2, true,  'ready',   repeat('4', 64), '2026-01-05T00:00:00Z', '2026-01-05T00:01:00Z'),
    (900102, repeat('5', 64), 'mod.pak',  8192, 0,     0, false, 'pending', '',              '2026-01-06T00:00:00Z', NULL);

INSERT INTO pak_owners (pak_id, user_id, filename, created_at) VALUES
    (900101, 900001, 'pak0.pak', '2026-01-05T00:00:00Z'),
    (900102, 900002, 'mod.pak',  '2026-01-06T00:00:00Z');

INSERT INTO pak_entries (pak_id, idx, name, filepos, filelen, sha256, kind) VALUES
    (900101, 0, 'maps/base1.bsp',      12,   1024, repeat('1', 64), 'bsp'),
    (900101, 1, 'textures/e1u1/x.wal', 1036, 2048, repeat('2', 64), 'wal');

INSERT INTO pak_assets (pak_id, sha256, role) VALUES
    (900101, repeat('1', 64), 'entry'),
    (900101, repeat('2', 64), 'png'),
    (900101, repeat('4', 64), 'manifest');

INSERT INTO paksets (id, name, owner_id, public, created_at, updated_at) VALUES
    ('demo',      'Demo',         NULL,   true,  '2026-01-05T00:00:00Z', '2026-01-05T00:00:00Z'),
    ('alice-mod', 'Alice''s mod', 900002, false, '2026-01-06T00:00:00Z', '2026-01-06T00:00:00Z');

INSERT INTO pakset_paks (pakset_id, position, pak_id) VALUES
    ('demo',      0, 900101),
    ('alice-mod', 0, 900101),
    ('alice-mod', 1, 900102);

INSERT INTO maps (pak_id, path, name, sha256, checksum, message, sky, info) VALUES
    (900101, 'maps/base1.bsp', 'base1', repeat('1', 64), 987654, 'Outer Base', 'unit1_', '{"spawns": 4}');

INSERT INTO jobs (id, kind, status, pak_id, user_id, progress, created_at, started_at, finished_at)
OVERRIDING SYSTEM VALUE VALUES
    (900201, 'ingest', 'done',   900101, 900001, 1, '2026-01-05T00:00:00Z', '2026-01-05T00:00:01Z', '2026-01-05T00:01:00Z'),
    (900202, 'ingest', 'queued', 900102, 900002, 0, '2026-01-06T00:00:00Z', NULL, NULL);

INSERT INTO saves (user_id, slot, comment, mapcmd, mode, schema, blob, size, created_at, updated_at) VALUES
    (900002, 'save0', 'Outer Base', 'base1', 'sp', 1, '\x00010203'::bytea, 4, '2026-02-01T00:00:00Z', '2026-02-01T00:00:00Z');

INSERT INTO settings (user_id, config, updated_at) VALUES
    (900002, 'bind w +forward', '2026-02-01T00:00:00Z');

INSERT INTO games (id, owner_id, mode, map, pakset_id, settings, public, started_at, ended_at) VALUES
    ('g-active', 900002, 'dm', 'base1', 'demo', '{"fraglimit": 20}', true,  '2026-02-01T00:00:00Z', NULL),
    ('g-ended',  900003, 'sp', 'base1', 'demo', '{}',                false, '2026-01-01T00:00:00Z', '2026-01-01T01:00:00Z');

INSERT INTO bans (id, user_id, ip, reason, created_by, created_at, expires_at)
OVERRIDING SYSTEM VALUE VALUES
    (900301, 900003, '',             'cheating', 900001, '2026-02-01T00:00:00Z', '2026-03-01T00:00:00Z'),
    (900302, NULL,   '198.51.100.7', 'spam',     900001, '2026-02-01T00:00:00Z', NULL);

INSERT INTO bot_spend (day, usd, updated_at) VALUES
    ('2026-02-01', 1.25, '2026-02-01T23:59:00Z');
