BEGIN;
SELECT plan(30);

SELECT has_table('users', 'users table exists');
SELECT has_table('sessions', 'sessions table exists');
SELECT has_table('blobs', 'blobs table exists');
SELECT has_table('paks', 'paks table exists');
SELECT has_table('pak_owners', 'pak_owners table exists');
SELECT has_table('pak_entries', 'pak_entries table exists');
SELECT has_table('pak_assets', 'pak_assets table exists');
SELECT has_table('paksets', 'paksets table exists');
SELECT has_table('pakset_paks', 'pakset_paks table exists');
SELECT has_table('maps', 'maps table exists');
SELECT has_table('jobs', 'jobs table exists');
SELECT has_table('saves', 'saves table exists');
SELECT has_table('settings', 'settings table exists');
SELECT has_table('games', 'games table exists');
SELECT has_table('bans', 'bans table exists');
SELECT has_table('bot_spend', 'bot_spend table exists');

SELECT col_is_pk('users', 'id', 'users.id is the primary key');
SELECT col_is_pk('sessions', 'id', 'sessions.id (token hash) is the primary key');
SELECT col_is_pk('pak_owners', ARRAY['pak_id', 'user_id'], 'pak_owners key is (pak_id, user_id)');
SELECT col_is_pk('pak_assets', ARRAY['pak_id', 'sha256'], 'pak_assets key is (pak_id, sha256)');
SELECT col_is_pk('saves', ARRAY['user_id', 'slot'], 'saves key is (user_id, slot)');
SELECT col_is_pk('bot_spend', 'day', 'bot_spend is keyed by day');

SELECT col_not_null('users', 'email', 'users.email is NOT NULL');
SELECT col_not_null('users', 'password_hash', 'users.password_hash is NOT NULL');
SELECT col_not_null('sessions', 'expires_at', 'sessions.expires_at is NOT NULL');
SELECT col_is_null('paks', 'ingested_at', 'paks.ingested_at is nullable');
SELECT col_is_null('bans', 'expires_at', 'bans.expires_at is nullable (permanent ban)');

SELECT has_index('users', 'users_email_key', 'users has the case-insensitive email index');
SELECT index_is_unique('users', 'users_email_key', 'users_email_key is unique');
SELECT has_index('pak_assets', 'pak_assets_sha256_idx', 'pak_assets has an index on sha256 for entitlement lookups');

SELECT * FROM finish();
ROLLBACK;
