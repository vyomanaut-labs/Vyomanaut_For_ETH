package org.vyomanaut.provider.storage

import org.vyomanaut.provider.config.DemoProfile

const val BYTES_PER_MIB = 1L shl 20
const val BYTES_PER_GIB = 1L shl 30

private const val PERCENT_BASE = 100L

sealed interface LimitRange {
    /** The provider may pick any limit from [minBytes] to [maxBytes]. */
    data class Available(val minBytes: Long, val maxBytes: Long) : LimitRange

    /** Free space is too small to offer the minimum limit. */
    data class NotEnoughSpace(val freeBytes: Long, val requiredFreeBytes: Long) : LimitRange
}

/**
 * Pure rules for how much storage a phone may offer. No Android types, so it is unit-tested on the host.
 *
 * The cap is a percentage of the capacity basis: free space plus the space this app already uses.
 * Counting the app's own usage keeps the cap stable as shards fill the offered space.
 */
object StorageLimitPolicy {

    fun maxAllowedBytes(
        freeBytes: Long,
        usedByAppBytes: Long = 0L,
        percent: Int = DemoProfile.MAX_LIMIT_PERCENT_OF_FREE,
    ): Long {
        val basis = freeBytes.coerceAtLeast(0L) + usedByAppBytes.coerceAtLeast(0L)
        val share = percent.toLong()
        // Split the division so a very large basis cannot overflow Long.
        return (basis / PERCENT_BASE) * share + (basis % PERCENT_BASE) * share / PERCENT_BASE
    }

    fun range(
        freeBytes: Long,
        usedByAppBytes: Long = 0L,
        percent: Int = DemoProfile.MAX_LIMIT_PERCENT_OF_FREE,
        minLimitBytes: Long = DemoProfile.MIN_LIMIT_BYTES,
    ): LimitRange {
        val max = maxAllowedBytes(freeBytes, usedByAppBytes, percent)
        if (max < minLimitBytes) {
            // Multiply first: dividing first would drop a few bytes and under-report the requirement.
            val requiredFree = minLimitBytes * PERCENT_BASE / percent
            return LimitRange.NotEnoughSpace(freeBytes.coerceAtLeast(0L), requiredFree)
        }
        return LimitRange.Available(minLimitBytes, max)
    }

    /** Forces a requested limit (or a previously saved one) into the allowed range. 0 when not allowed at all. */
    fun clamp(requestedBytes: Long, range: LimitRange): Long = when (range) {
        is LimitRange.Available -> requestedBytes.coerceIn(range.minBytes, range.maxBytes)
        is LimitRange.NotEnoughSpace -> 0L
    }
}
