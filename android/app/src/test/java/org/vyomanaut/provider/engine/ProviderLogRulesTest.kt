package org.vyomanaut.provider.engine

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ProviderLogRulesTest {

    // Real lines printed by internal/p2p/heartbeat.go.
    private val departedLine =
        """2026/11/06 10:00:00 [heartbeat] microservice returned 403: {"error_code":"PROVIDER_DEPARTED","message":"provider has departed"}"""
    private val unauthorizedLine =
        """2026/11/06 10:00:00 [heartbeat] microservice returned 401: {"error_code":"UNAUTHORIZED","message":"invalid token"}"""
    private val mismatchLine =
        "2026/11/06 10:00:00 [heartbeat] 401 means this provider's saved registration does not match the coordinator. " +
            "This is expected if the coordinator was restarted since this provider onboarded."

    @Test
    fun departedReplyIsRecognised() {
        assertEquals(Rejection.DEPARTED, ProviderLogRules.rejectionIn(departedLine))
    }

    @Test
    fun mismatchIsRecognisedFromTheExplanationLineOnly() {
        assertEquals(Rejection.UNKNOWN_TO_COORDINATOR, ProviderLogRules.rejectionIn(mismatchLine))
        // The bare 401 line comes with the explanation line; counting both would act on a single heartbeat.
        assertNull(ProviderLogRules.rejectionIn(unauthorizedLine))
    }

    @Test
    fun ordinaryLinesAreIgnored() {
        assertNull(ProviderLogRules.rejectionIn("[heartbeat] send failed; will retry next cycle: dial tcp: i/o timeout"))
        assertNull(ProviderLogRules.rejectionIn("[AUDIT] answered challenge in 41 ms"))
    }

    @Test
    fun departedActsAtOnceButMismatchWaitsForASecondHeartbeat() {
        assertTrue(ProviderLogRules.shouldAct(Rejection.DEPARTED, 1))
        assertFalse(ProviderLogRules.shouldAct(Rejection.UNKNOWN_TO_COORDINATOR, 1))
        assertTrue(ProviderLogRules.shouldAct(Rejection.UNKNOWN_TO_COORDINATOR, 2))
    }

    @Test
    fun phoneAlreadyRegisteredIsRecognised() {
        assertTrue(
            ProviderLogRules.isPhoneTaken(
                """register: register returned HTTP 409: {"error_code":"PHONE_ALREADY_REGISTERED","message":"phone number already registered"}""",
            ),
        )
        assertFalse(ProviderLogRules.isPhoneTaken("otp verify returned HTTP 400"))
    }
}
