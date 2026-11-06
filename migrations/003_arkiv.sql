-- migrations/003_arkiv.sql
--
-- Arkiv mirror feature (hackathon build). Number 003 is owned by the Arkiv
-- branch (see migrations/README.md); 002 belongs to the chain/payments branch.
--
-- Profile-invariant, hand-written, applied after 001 by
-- scripts/ci/apply_extra_migrations.sh. Never edit once committed.
--
-- What this adds: bookkeeping for the optional Arkiv publisher
-- (internal/arkiv, cmd/microservice/arkiv_ext.go). Postgres stays the source of
-- truth; these two tables only record what has already been mirrored to Arkiv.
--
-- Rules followed (migrations/README.md): the tables are new and owned by this
-- file; GRANT only what vyomanaut_app needs; audit_receipts and escrow_events
-- are not touched; no FORCE ROW LEVEL SECURITY because rows are updated in
-- place (SENT -> CONFIRMED), so these tables are deliberately not append-only.

-- ── arkiv_publications ─────────────────────────────────────────────────────────
-- One row per logical Arkiv entity the publisher has decided to write.
--
-- Lifecycle: the publisher INSERTs a row with status SENT *before* sending the
-- transaction, then UPDATEs it to CONFIRMED (entity_key and tx_hash set) once
-- the receipt shows the EntityCreated log, or to FAILED with last_error. A row
-- left in SENT after a crash is retried; the retry may create a second entity
-- on chain, which is harmless because batches are immutable and the verifier
-- drops duplicate receipt_ids.
--
-- (kind, reference) is unique so one logical item is never reserved twice:
--   audit_batch    reference = '<provider_id>:<period_start_unix>:<seq>'
--   file_manifest  reference = '<file_id>'
--   file_tombstone reference = '<file_id>'
CREATE TABLE IF NOT EXISTS arkiv_publications (
    publication_id  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),

    kind            TEXT            NOT NULL
                    CHECK (kind IN ('audit_batch', 'file_manifest', 'file_tombstone')),

    reference       TEXT            NOT NULL
                    CHECK (char_length(reference) BETWEEN 1 AND 200),

    status          TEXT            NOT NULL DEFAULT 'SENT'
                    CHECK (status IN ('SENT', 'CONFIRMED', 'FAILED')),

    tx_hash         BYTEA           CHECK (tx_hash IS NULL OR octet_length(tx_hash) = 32),
    entity_key      BYTEA           CHECK (entity_key IS NULL OR octet_length(entity_key) = 32),

    -- Audit batches only: the service_countersign_ts range the batch covers.
    range_start     TIMESTAMPTZ,
    range_end       TIMESTAMPTZ,

    item_count      INT             CHECK (item_count IS NULL OR item_count >= 0),
    payload_bytes   INT             CHECK (payload_bytes IS NULL OR payload_bytes > 0),

    gas_used        BIGINT          CHECK (gas_used IS NULL OR gas_used >= 0),
    fee_wei         NUMERIC(40, 0)  CHECK (fee_wei IS NULL OR fee_wei >= 0),

    attempts        INT             NOT NULL DEFAULT 1 CHECK (attempts >= 1),
    last_error      TEXT,

    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT arkiv_publications_kind_reference_key UNIQUE (kind, reference),

    -- A confirmed row always carries the proof of what it confirmed.
    CONSTRAINT arkiv_publications_confirmed_complete CHECK (
        status <> 'CONFIRMED' OR (entity_key IS NOT NULL AND tx_hash IS NOT NULL)
    ),

    CONSTRAINT arkiv_publications_range_ordered CHECK (
        range_start IS NULL OR range_end IS NULL OR range_start <= range_end
    )
);

-- The publisher's retry scan: unfinished rows, oldest first.
CREATE INDEX IF NOT EXISTS idx_arkiv_publications_unfinished
    ON arkiv_publications (created_at)
    WHERE status <> 'CONFIRMED';

-- arkivcheck and dashboards: latest confirmed batches by kind.
CREATE INDEX IF NOT EXISTS idx_arkiv_publications_kind_created
    ON arkiv_publications (kind, created_at DESC);

COMMENT ON TABLE arkiv_publications IS
    'Ledger of entities mirrored to Arkiv. Postgres remains the source of truth; '
    'this records what was published, its entity key and its fee. A row left in '
    'SENT after a crash is retried (duplicates on chain are harmless).';

-- ── arkiv_cursors ──────────────────────────────────────────────────────────────
-- High-water marks so the publisher resumes where it stopped after a restart.
-- audit_receipts is read by service_countersign_ts, trailing real time by a
-- 30-second settling lag, because Phase 2 commits can land slightly out of
-- timestamp order. The publisher inserts its own row on first start.
CREATE TABLE IF NOT EXISTS arkiv_cursors (
    stream          TEXT            PRIMARY KEY
                    CHECK (stream IN ('audit_receipts', 'files_registered', 'files_deleted')),
    high_water      TIMESTAMPTZ     NOT NULL,
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE arkiv_cursors IS
    'Per-stream high-water mark for the Arkiv publisher. One row per stream.';

-- ── Privileges ─────────────────────────────────────────────────────────────────
-- vyomanaut_app is the coordinator's request-path role; the publisher runs
-- inside the coordinator on that pool. No DELETE: rows are never removed.
GRANT SELECT, INSERT, UPDATE ON arkiv_publications TO vyomanaut_app;
GRANT SELECT, INSERT, UPDATE ON arkiv_cursors      TO vyomanaut_app;
