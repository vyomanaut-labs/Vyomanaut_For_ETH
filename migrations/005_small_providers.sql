-- 005_small_providers.sql
--
-- Lets a provider declare as little as 1 GB (was 10). Phones and laptops that
-- join a demo network often have only a few GB to spare, and the 10 GB floor
-- forced the app to register them as 10 GB they did not offer.
--
-- 001 keeps its original CHECK (committed migrations are never edited); this
-- replaces it. The name is the one Postgres gives an inline column CHECK.
-- Profile-invariant. Applied after 004 by scripts/ci/apply_extra_migrations.sh.

ALTER TABLE providers DROP CONSTRAINT IF EXISTS providers_declared_storage_gb_check;
ALTER TABLE providers
    ADD CONSTRAINT providers_declared_storage_gb_check
    CHECK (declared_storage_gb BETWEEN 1 AND 100000);
