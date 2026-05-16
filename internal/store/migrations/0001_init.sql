-- 0001_init: initial schema for photo-server (kgu.9).
--
-- Time columns are INTEGER Unix epoch seconds (UTC): sortable,
-- timezone-safe, and free of driver datetime-format quirks.

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,          -- opaque random token (kgu.14)
    display_name TEXT NOT NULL DEFAULT '',  -- optional; '' renders as "Anonymous"
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

CREATE TABLE photos (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    content_hash        TEXT    NOT NULL UNIQUE,          -- dedup (kgu.10/11)
    mime                TEXT    NOT NULL,
    original_filename   TEXT    NOT NULL DEFAULT '',
    uploader_session_id TEXT    REFERENCES sessions(id),  -- nullable
    display_name        TEXT    NOT NULL DEFAULT '',      -- denormalised at upload time
    taken_at            INTEGER,                          -- EXIF DateTimeOriginal (kgu.11), nullable
    uploaded_at         INTEGER NOT NULL,
    width               INTEGER NOT NULL DEFAULT 0,
    height              INTEGER NOT NULL DEFAULT 0,
    hidden_at           INTEGER                           -- NULL = visible; admin hide (kgu.19)
);

-- Gallery: reverse-chronological over visible photos only (PRD F8).
CREATE INDEX idx_photos_visible_recent
    ON photos (uploaded_at DESC)
    WHERE hidden_at IS NULL;

-- "Your own recent uploads" (kgu.16) and admin per-uploader views.
CREATE INDEX idx_photos_session
    ON photos (uploader_session_id);
