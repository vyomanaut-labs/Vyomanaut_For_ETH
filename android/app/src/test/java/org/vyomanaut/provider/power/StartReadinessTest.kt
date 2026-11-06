package org.vyomanaut.provider.power

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class StartReadinessTest {

    @Test
    fun readyWhenBothExemptionsAreDone() {
        assertTrue(StartReadiness.missingSteps(appBatteryExempt = true, zeroTierExemptConfirmed = true).isEmpty())
    }

    @Test
    fun appExemptionIsListedFirst() {
        assertEquals(
            listOf(StartReadiness.NEED_APP_EXEMPTION, StartReadiness.NEED_ZEROTIER_EXEMPTION),
            StartReadiness.missingSteps(appBatteryExempt = false, zeroTierExemptConfirmed = false),
        )
    }

    @Test
    fun zeroTierStepIsNotNeededOnASharedWifi() {
        assertTrue(
            StartReadiness.missingSteps(appBatteryExempt = true, zeroTierExemptConfirmed = false, usesZeroTier = false).isEmpty(),
        )
    }

    @Test
    fun zeroTierAloneStillBlocksStart() {
        assertEquals(
            listOf(StartReadiness.NEED_ZEROTIER_EXEMPTION),
            StartReadiness.missingSteps(appBatteryExempt = true, zeroTierExemptConfirmed = false),
        )
    }
}
