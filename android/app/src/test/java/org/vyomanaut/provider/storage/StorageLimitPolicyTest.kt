package org.vyomanaut.provider.storage

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class StorageLimitPolicyTest {

    @Test
    fun maxAllowed_is_half_of_free() {
        assertEquals(50 * BYTES_PER_GIB, StorageLimitPolicy.maxAllowedBytes(100 * BYTES_PER_GIB))
    }

    @Test
    fun maxAllowed_counts_space_already_used_by_the_app() {
        // 90 GB free + 10 GB already stored by the app: basis 100 GB, cap 50 GB.
        val cap = StorageLimitPolicy.maxAllowedBytes(90 * BYTES_PER_GIB, usedByAppBytes = 10 * BYTES_PER_GIB)
        assertEquals(50 * BYTES_PER_GIB, cap)
    }

    @Test
    fun maxAllowed_never_negative() {
        assertEquals(0L, StorageLimitPolicy.maxAllowedBytes(-5L))
    }

    @Test
    fun maxAllowed_does_not_overflow_on_huge_values() {
        val cap = StorageLimitPolicy.maxAllowedBytes(Long.MAX_VALUE / 2)
        assertTrue(cap > 0)
    }

    @Test
    fun range_available_when_half_reaches_one_gib() {
        val range = StorageLimitPolicy.range(2 * BYTES_PER_GIB)
        assertEquals(LimitRange.Available(BYTES_PER_GIB, BYTES_PER_GIB), range)
    }

    @Test
    fun range_not_enough_space_below_two_gib_free() {
        val range = StorageLimitPolicy.range(BYTES_PER_GIB)
        assertEquals(LimitRange.NotEnoughSpace(BYTES_PER_GIB, 2 * BYTES_PER_GIB), range)
    }

    @Test
    fun clamp_caps_a_too_large_request_at_the_maximum() {
        val range = StorageLimitPolicy.range(100 * BYTES_PER_GIB)
        assertEquals(50 * BYTES_PER_GIB, StorageLimitPolicy.clamp(80 * BYTES_PER_GIB, range))
    }

    @Test
    fun clamp_raises_a_too_small_request_to_the_minimum() {
        val range = StorageLimitPolicy.range(100 * BYTES_PER_GIB)
        assertEquals(BYTES_PER_GIB, StorageLimitPolicy.clamp(0L, range))
    }

    @Test
    fun clamp_is_zero_when_not_enough_space() {
        val range = StorageLimitPolicy.range(BYTES_PER_GIB)
        assertEquals(0L, StorageLimitPolicy.clamp(BYTES_PER_GIB, range))
    }
}
