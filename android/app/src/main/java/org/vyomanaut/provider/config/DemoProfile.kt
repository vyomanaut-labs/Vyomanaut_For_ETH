package org.vyomanaut.provider.config

/**
 * Demo network profile values, mirrored from the Go provider's NetworkProfile (demo literal).
 * The prototype runs the demo profile only; the coordinator remains the source of truth.
 */
object DemoProfile {
    const val DATA_SHARDS = 3
    const val TOTAL_SHARDS = 5
    const val MIN_ACTIVE_PROVIDERS = 5
    const val SHARD_SIZE_BYTES = 262_144
    const val AUDIT_PERIOD_MINUTES = 2
    const val DEPARTURE_THRESHOLD_MINUTES = 10
    const val DEFAULT_LISTEN_PORT = 30303

    /**
     * The provider may offer at most this share of the phone's currently free space. Was 10, which needed
     * 10 GB free to offer the smallest limit; at 50 a phone with 2 GB free can join.
     */
    const val MAX_LIMIT_PERCENT_OF_FREE = 50

    /** Below this limit a phone is not worth admitting (1 GiB). */
    const val MIN_LIMIT_BYTES = 1L shl 30

    /**
     * The smallest size the coordinator accepts (API check and database CHECK, migration 005). Older
     * coordinators required 10; a phone is never registered as more than it offers.
     */
    const val COORDINATOR_MIN_DECLARED_GB = 1
}
