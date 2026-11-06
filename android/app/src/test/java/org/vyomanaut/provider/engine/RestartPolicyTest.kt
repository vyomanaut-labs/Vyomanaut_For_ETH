package org.vyomanaut.provider.engine

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RestartPolicyTest {

    @Test
    fun delaysGrowThenCap() {
        assertEquals(listOf(5L, 15L, 30L, 60L, 120L, 120L, 120L), (1..7).map(RestartPolicy::delaySeconds))
    }

    @Test
    fun quickFailuresCountUp() {
        assertEquals(1, RestartPolicy.nextAttempt(previousAttempt = 0, uptimeMs = 1_000))
        assertEquals(4, RestartPolicy.nextAttempt(previousAttempt = 3, uptimeMs = 1_000))
    }

    @Test
    fun aLongHealthyRunResetsTheCount() {
        assertEquals(1, RestartPolicy.nextAttempt(previousAttempt = 7, uptimeMs = RestartPolicy.STABLE_AFTER_MS))
    }

    @Test
    fun givesUpOnlyAfterTheLastAttempt() {
        assertFalse(RestartPolicy.shouldGiveUp(RestartPolicy.MAX_ATTEMPTS))
        assertTrue(RestartPolicy.shouldGiveUp(RestartPolicy.MAX_ATTEMPTS + 1))
    }

    @Test
    fun outOfRangeAttemptsAreClamped() {
        assertEquals(5L, RestartPolicy.delaySeconds(0))
        assertEquals(120L, RestartPolicy.delaySeconds(1_000))
    }
}
