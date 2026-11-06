-- 004_whitechain_refunds.sql
--
-- Refunds observed on chain. VyomaEscrow.refund(owner) returns an owner's
-- unspent on-chain credit once the coordinator has been silent for the
-- contract's silence timeout. The money leaves the escrow without the
-- coordinator's help, so the paise ledger must be told: each Refunded log is
-- recorded here once and becomes one owner WITHDRAWAL in owner_escrow_events.
--
-- Mirrors chain_deposits (002): a log is recorded whether or not its wallet is
-- bound, and is applied to the ledger when an owner is known. A deposit and a
-- refund from a wallet bound later are both applied then, so they cancel.
-- Profile-invariant. Applied after 002 by scripts/ci/apply_extra_migrations.sh.

CREATE TABLE chain_refunds (
    tx_hash      BYTEA       NOT NULL CHECK (octet_length(tx_hash) = 32),
    log_index    BIGINT      NOT NULL CHECK (log_index >= 0),
    wallet       BYTEA       NOT NULL CHECK (octet_length(wallet) = 20),
    amount_micro BIGINT      NOT NULL CHECK (amount_micro > 0),
    block_number BIGINT      NOT NULL CHECK (block_number >= 0),
    owner_id     UUID        REFERENCES owners(owner_id),
    amount_paise BIGINT      CHECK (amount_paise >= 0),
    applied_at   TIMESTAMPTZ,
    seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tx_hash, log_index),
    CHECK ((applied_at IS NULL) = (amount_paise IS NULL AND owner_id IS NULL))
);

CREATE INDEX chain_refunds_unapplied ON chain_refunds (wallet) WHERE applied_at IS NULL;

GRANT SELECT, INSERT, UPDATE ON chain_refunds TO vyomanaut_app;
