-- +goose Up
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    display_name  text        NOT NULL,
    is_admin      boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- id is the hex SHA-256 of the cookie token (the token itself is never stored)
CREATE TABLE sessions (
    id         text        PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    user_agent text        NOT NULL DEFAULT '',
    ip         text        NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- content-addressed blobs (raw pak entries, PNGs, palettes, manifests, paks)
CREATE TABLE blobs (
    sha256       text        PRIMARY KEY CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size         bigint      NOT NULL,
    content_type text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE paks (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sha256          text        NOT NULL UNIQUE,
    name            text        NOT NULL,
    size            bigint      NOT NULL,
    checksum        bigint      NOT NULL DEFAULT 0,
    num_files       integer     NOT NULL DEFAULT 0,
    public          boolean     NOT NULL DEFAULT false,
    status          text        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'ingesting', 'ready', 'failed')),
    error           text        NOT NULL DEFAULT '',
    manifest_sha256 text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    ingested_at     timestamptz
);

CREATE TABLE pak_owners (
    pak_id     bigint      NOT NULL REFERENCES paks (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    filename   text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pak_id, user_id)
);
CREATE INDEX pak_owners_user_id_idx ON pak_owners (user_id);

CREATE TABLE pak_entries (
    pak_id  bigint  NOT NULL REFERENCES paks (id) ON DELETE CASCADE,
    idx     integer NOT NULL,
    name    text    NOT NULL,
    filepos integer NOT NULL,
    filelen integer NOT NULL,
    sha256  text    NOT NULL,
    kind    text    NOT NULL,
    PRIMARY KEY (pak_id, idx)
);
CREATE INDEX pak_entries_sha256_idx ON pak_entries (sha256);

-- every blob a pak makes available (raw entries + derived artifacts);
-- the entitlement check of GET /assets/{sha256} (ADR-0005) runs on this.
CREATE TABLE pak_assets (
    pak_id bigint NOT NULL REFERENCES paks (id) ON DELETE CASCADE,
    sha256 text   NOT NULL,
    role   text   NOT NULL,
    PRIMARY KEY (pak_id, sha256)
);
CREATE INDEX pak_assets_sha256_idx ON pak_assets (sha256);

CREATE TABLE paksets (
    id         text        PRIMARY KEY,
    name       text        NOT NULL,
    owner_id   bigint      REFERENCES users (id) ON DELETE CASCADE, -- NULL: system (demo)
    public     boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE pakset_paks (
    pakset_id text    NOT NULL REFERENCES paksets (id) ON DELETE CASCADE,
    position  integer NOT NULL,
    pak_id    bigint  NOT NULL REFERENCES paks (id) ON DELETE RESTRICT,
    PRIMARY KEY (pakset_id, position)
);

CREATE TABLE maps (
    pak_id   bigint NOT NULL REFERENCES paks (id) ON DELETE CASCADE,
    path     text   NOT NULL,
    name     text   NOT NULL,
    sha256   text   NOT NULL,
    checksum bigint NOT NULL,
    message  text   NOT NULL DEFAULT '',
    sky      text   NOT NULL DEFAULT '',
    info     jsonb  NOT NULL DEFAULT '{}',
    PRIMARY KEY (pak_id, path)
);
CREATE INDEX maps_name_idx ON maps (name);

CREATE TABLE jobs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text        NOT NULL,
    status      text        NOT NULL DEFAULT 'queued'
                CHECK (status IN ('queued', 'running', 'done', 'failed')),
    pak_id      bigint      REFERENCES paks (id) ON DELETE CASCADE,
    user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    progress    real        NOT NULL DEFAULT 0,
    error       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz
);
CREATE INDEX jobs_pak_id_idx ON jobs (pak_id);

-- server-side savegames (ADR-0004); blob is the encoded save slot
CREATE TABLE saves (
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slot       text        NOT NULL,
    comment    text        NOT NULL DEFAULT '',
    mapcmd     text        NOT NULL DEFAULT '',
    mode       text        NOT NULL DEFAULT '',
    schema     integer     NOT NULL DEFAULT 0,
    blob       bytea       NOT NULL,
    size       integer     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, slot)
);

-- per-account config.cfg text
CREATE TABLE settings (
    user_id    bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    config     text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- persisted registry of game instances
CREATE TABLE games (
    id         text        PRIMARY KEY,
    owner_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    mode       text        NOT NULL,
    map        text        NOT NULL,
    pakset_id  text        NOT NULL DEFAULT '',
    settings   jsonb       NOT NULL DEFAULT '{}',
    public     boolean     NOT NULL DEFAULT false,
    started_at timestamptz NOT NULL DEFAULT now(),
    ended_at   timestamptz
);
CREATE INDEX games_active_idx ON games (started_at) WHERE ended_at IS NULL;

CREATE TABLE bans (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      REFERENCES users (id) ON DELETE CASCADE,
    ip         text        NOT NULL DEFAULT '',
    reason     text        NOT NULL DEFAULT '',
    created_by bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    CHECK (user_id IS NOT NULL OR ip <> '')
);
CREATE INDEX bans_user_id_idx ON bans (user_id);
CREATE INDEX bans_ip_idx ON bans (ip) WHERE ip <> '';

-- +goose Down
DROP TABLE bans;
DROP TABLE games;
DROP TABLE settings;
DROP TABLE saves;
DROP TABLE jobs;
DROP TABLE maps;
DROP TABLE pakset_paks;
DROP TABLE paksets;
DROP TABLE pak_assets;
DROP TABLE pak_entries;
DROP TABLE pak_owners;
DROP TABLE paks;
DROP TABLE blobs;
DROP TABLE sessions;
DROP TABLE users;
