-- 0002_hearts: anonymous per-session hearts + denormalized count (kgu.23).
--
-- A heart is (photo, guest-session). The composite PK gives idempotent
-- one-heart-per-session toggling. heart_count is denormalized onto
-- photos so the gallery sort + tile render need no COUNT()/join.
-- ON DELETE CASCADE (foreign_keys is ON) reaps hearts when a photo is
-- deleted by the admin.

CREATE TABLE hearts (
    photo_id   INTEGER NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    session_id TEXT    NOT NULL REFERENCES sessions(id),
    created_at INTEGER NOT NULL,
    PRIMARY KEY (photo_id, session_id)
);

ALTER TABLE photos ADD COLUMN heart_count INTEGER NOT NULL DEFAULT 0;

-- "Most loved" leaderboard: visible photos by hearts, then recency.
CREATE INDEX idx_photos_top
    ON photos (heart_count DESC, id DESC)
    WHERE hidden_at IS NULL;
