package org.vyomanaut.provider.power

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SetupStepsTest {

    private val fresh = SetupInputs(
        joined = false, coordinatorReachable = null, storageSaved = false, appBatteryExempt = false,
        usesZeroTier = false, zeroTierExempt = false, oemBrand = null, oemDone = false,
        binaryAvailable = true, registered = false, registering = false, running = false, online = false,
    )
    private val ready = fresh.copy(joined = true, coordinatorReachable = true, storageSaved = true, appBatteryExempt = true)

    @Test
    fun a_new_phone_starts_with_joining() {
        val next = SetupSteps.next(SetupSteps.steps(fresh))!!
        assertEquals("Join a network", next.title)
        assertEquals(StepAction.OPEN_NETWORK, next.action)
    }

    @Test
    fun storage_offers_a_one_tap_default() {
        val steps = SetupSteps.steps(fresh.copy(joined = true, coordinatorReachable = true))
        assertEquals(StepAction.USE_DEFAULT_STORAGE, SetupSteps.next(steps)!!.action)
    }

    @Test
    fun too_little_space_sends_to_the_storage_tab() {
        val steps = SetupSteps.steps(fresh.copy(joined = true, coordinatorReachable = true, storageShortBy = "2 GB"))
        val next = SetupSteps.next(steps)!!
        assertEquals(StepAction.OPEN_STORAGE, next.action)
        assertTrue(next.detail.contains("2 GB"))
    }

    @Test
    fun an_unreachable_coordinator_can_be_checked_again() {
        val steps = SetupSteps.steps(fresh.copy(joined = true, coordinatorReachable = false, coordinatorProblem = "Timed out."))
        val next = SetupSteps.next(steps)!!
        assertEquals(StepAction.RECHECK, next.action)
        assertTrue(next.detail.startsWith("Timed out."))
    }

    @Test
    fun register_is_offered_only_when_everything_before_it_is_done() {
        assertEquals(StepAction.REGISTER, SetupSteps.next(SetupSteps.steps(ready))!!.action)
        val noBattery = SetupSteps.steps(ready.copy(appBatteryExempt = false))
        assertEquals(StepAction.BATTERY, SetupSteps.next(noBattery)!!.action)
        assertEquals(StepState.BLOCKED, noBattery.first { it.title == "Register this phone" }.state)
    }

    @Test
    fun zerotier_step_appears_only_over_zerotier() {
        assertFalse(SetupSteps.steps(ready).any { it.action == StepAction.ZEROTIER })
        val zt = SetupSteps.steps(ready.copy(usesZeroTier = true))
        assertEquals(StepAction.ZEROTIER, SetupSteps.next(zt)!!.action)
    }

    @Test
    fun phone_maker_settings_are_recommended_not_required() {
        val steps = SetupSteps.steps(ready.copy(oemBrand = "Samsung"))
        assertEquals(StepAction.REGISTER, SetupSteps.next(steps)!!.action)
        val (_, total) = SetupSteps.progress(steps)
        assertEquals(SetupSteps.steps(ready).size, total)
    }

    @Test
    fun while_waiting_for_the_code_the_next_step_says_so() {
        val next = SetupSteps.next(SetupSteps.steps(ready.copy(registering = true)))!!
        assertEquals(StepState.WAITING, next.state)
        assertTrue(next.detail.contains("join desk"))
    }

    @Test
    fun a_stopped_registered_phone_is_offered_start() {
        val next = SetupSteps.next(SetupSteps.steps(ready.copy(registered = true)))!!
        assertEquals(StepAction.START, next.action)
    }

    @Test
    fun online_means_all_done() {
        val steps = SetupSteps.steps(ready.copy(registered = true, running = true, online = true))
        assertNull(SetupSteps.next(steps))
        val (done, total) = SetupSteps.progress(steps)
        assertEquals(total, done)
    }
}
