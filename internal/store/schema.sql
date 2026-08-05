-- Schema for the CMIYC pool. Applied idempotently on server startup, so it
-- doubles as the migration: adding a column later means adding an
-- "ALTER TABLE ... IF NOT EXISTS" block here.
--
-- Design notes:
--   cracks               : the unique source of truth. One row per distinct
--                          cracked line. This is the ONLY table that feeds a
--                          contest submission. submitted_at IS NULL == pending.
--   crack_contributors   : bounded set (crack_id, author). Records every
--                          distinct teammate who has ever uploaded a given
--                          line, exactly once each. Idempotent full re-uploads
--                          do not grow it. This is the post-competition
--                          analytics surface: first-crack attribution lives on
--                          cracks.first_by, independent-overlap lives here.

CREATE TABLE IF NOT EXISTS cracks (
    id            BIGSERIAL   PRIMARY KEY,
    line          TEXT        NOT NULL,
    line_hash     BYTEA       NOT NULL UNIQUE,          -- sha256(line)
    first_by      TEXT        NOT NULL,                 -- race winner (attribution)
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitted_at  TIMESTAMPTZ                           -- NULL => not yet sent to KoreLogic
);

-- Pending scans (plain pull) hit this constantly.
CREATE INDEX IF NOT EXISTS cracks_pending_idx
    ON cracks (id) WHERE submitted_at IS NULL;

CREATE TABLE IF NOT EXISTS crack_contributors (
    crack_id      BIGINT      NOT NULL REFERENCES cracks(id) ON DELETE CASCADE,
    author        TEXT        NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (crack_id, author)
);

CREATE INDEX IF NOT EXISTS crack_contributors_author_idx
    ON crack_contributors (author);
