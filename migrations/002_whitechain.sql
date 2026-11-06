-- 002_whitechain.sql
--
-- Settlement of provider payouts on an EVM chain, and the wallet bindings and
-- audit-anchor bookkeeping it needs. Profile-invariant: one file serves the
-- demo and production schemas. Applied after 001 by
-- scripts/ci/apply_extra_migrations.sh.
--
-- The paise ledger (escrow_events, owner_escrow_events) stays the source of
-- truth. Nothing here changes it. These tables only record what has been
-- mirrored to the chain, so every step is idempotent and a crash between two
-- steps can be resumed without paying twice.
--
-- Amounts on chain are micro-units of a 6-decimal stablecoin held in BIGINT.
-- The ledger stays in paise; the conversion factor is a coordinator setting.

-- ── Wallet bindings ──────────────────────────────────────────────────────────

-- An owner's chain wallet. A deposit made on chain is credited to the owner
-- whose wallet it came from.
CREATE TABLE owner_wallets (
    owner_id  UUID        PRIMARY KEY REFERENCES owners(owner_id),
    wallet    BYTEA       NOT NULL UNIQUE CHECK (octet_length(wallet) = 20),
    bound_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Where a provider's earnings are paid. Several providers may share a wallet
-- (one lab, one payout address), so the wallet is not unique.
CREATE TABLE provider_payout_addresses (
    provider_id UUID        PRIMARY KEY REFERENCES providers(provider_id),
    wallet      BYTEA       NOT NULL CHECK (octet_length(wallet) = 20),
    bound_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Deposits observed on chain ───────────────────────────────────────────────

-- One row per Deposited log. A log is recorded once, whether or not its wallet
-- is bound yet; it is credited to the ledger when an owner is known.
CREATE TABLE chain_deposits (
    tx_hash      BYTEA       NOT NULL CHECK (octet_length(tx_hash) = 32),
    log_index    BIGINT      NOT NULL CHECK (log_index >= 0),
    wallet       BYTEA       NOT NULL CHECK (octet_length(wallet) = 20),
    amount_micro BIGINT      NOT NULL CHECK (amount_micro > 0),
    block_number BIGINT      NOT NULL CHECK (block_number >= 0),
    owner_id     UUID        REFERENCES owners(owner_id),
    amount_paise BIGINT      CHECK (amount_paise >= 0),
    credited_at  TIMESTAMPTZ,
    seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tx_hash, log_index),
    CHECK ((credited_at IS NULL) = (amount_paise IS NULL AND owner_id IS NULL))
);

CREATE INDEX chain_deposits_uncredited ON chain_deposits (wallet) WHERE credited_at IS NULL;

-- Where the log scanner stopped, per named stream.
CREATE TABLE chain_cursors (
    name         TEXT        PRIMARY KEY,
    block_number BIGINT      NOT NULL CHECK (block_number >= 0),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Payout periods ───────────────────────────────────────────────────────────

-- One billing period posted to the escrow contract.
--   RESERVED  the payout list is fixed in this database but not yet on chain
--   POSTED    on chain, waiting out the challenge window or the claim
--   CLAIMED   every payee has been paid
--   FAILED    abandoned; its releases are free to be batched again
CREATE TABLE chain_periods (
    period       BIGINT      PRIMARY KEY CHECK (period > 0),
    status       TEXT        NOT NULL CHECK (status IN ('RESERVED', 'POSTED', 'CLAIMED', 'FAILED')),
    ref          BYTEA       NOT NULL CHECK (octet_length(ref) = 32),
    total_micro  BIGINT      NOT NULL CHECK (total_micro > 0),
    post_tx      BYTEA       CHECK (octet_length(post_tx) = 32),
    claimable_at TIMESTAMPTZ,
    claim_tx     BYTEA       CHECK (octet_length(claim_tx) = 32),
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The ledger RELEASE rows a period pays. A release can belong to at most one
-- period, which is what stops it being paid twice. Deleting a FAILED period's
-- items frees its releases for a later period.
CREATE TABLE chain_period_items (
    release_event_id UUID   PRIMARY KEY REFERENCES escrow_events(event_id),
    period           BIGINT NOT NULL REFERENCES chain_periods(period),
    provider_id      UUID   NOT NULL REFERENCES providers(provider_id),
    amount_paise     BIGINT NOT NULL CHECK (amount_paise > 0)
);

CREATE INDEX chain_period_items_period ON chain_period_items (period);

-- What each wallet is paid in a period.
CREATE TABLE chain_period_payouts (
    period       BIGINT NOT NULL REFERENCES chain_periods(period),
    wallet       BYTEA  NOT NULL CHECK (octet_length(wallet) = 20),
    amount_micro BIGINT NOT NULL CHECK (amount_micro > 0),
    PRIMARY KEY (period, wallet)
);

-- What each owner's on-chain credit is debited in a period.
CREATE TABLE chain_period_debits (
    period       BIGINT NOT NULL REFERENCES chain_periods(period),
    wallet       BYTEA  NOT NULL CHECK (octet_length(wallet) = 20),
    amount_micro BIGINT NOT NULL CHECK (amount_micro > 0),
    PRIMARY KEY (period, wallet)
);

CREATE INDEX chain_period_debits_wallet ON chain_period_debits (wallet);

-- ── Audit anchors ────────────────────────────────────────────────────────────

-- One Merkle root of finished audit receipts per epoch. last_countersign_ts and
-- last_receipt_id name the final receipt in the epoch, so the next epoch
-- continues exactly after it in the canonical receipt order.
--   RESERVED  root computed and recorded, not yet on chain
--   ANCHORED  on chain
--   FAILED    abandoned; the next epoch starts from the same place
CREATE TABLE chain_anchors (
    epoch                BIGINT      PRIMARY KEY CHECK (epoch > 0),
    status               TEXT        NOT NULL CHECK (status IN ('RESERVED', 'ANCHORED', 'FAILED')),
    root                 BYTEA       NOT NULL CHECK (octet_length(root) = 32),
    leaf_count           BIGINT      NOT NULL CHECK (leaf_count > 0),
    first_countersign_ts TIMESTAMPTZ NOT NULL,
    last_countersign_ts  TIMESTAMPTZ NOT NULL,
    last_receipt_id      UUID        NOT NULL,
    anchor_tx            BYTEA       CHECK (octet_length(anchor_tx) = 32),
    last_error           TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Privileges ───────────────────────────────────────────────────────────────

GRANT SELECT, INSERT, UPDATE ON
    owner_wallets, provider_payout_addresses, chain_deposits, chain_cursors,
    chain_periods, chain_period_payouts, chain_period_debits, chain_anchors
    TO vyomanaut_app;

GRANT SELECT, INSERT, DELETE ON chain_period_items TO vyomanaut_app;
