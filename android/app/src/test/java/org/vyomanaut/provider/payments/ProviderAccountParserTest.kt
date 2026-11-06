package org.vyomanaut.provider.payments

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ProviderAccountParserTest {

    @Test
    fun readsTheRegistrationFile() {
        val reg = ProviderAccountParser.registration(
            """{"provider_id":"6f1c2b9e-0000-4000-8000-000000000001","token":"eyJ.abc.def","declared_storage_gb":10}""",
        )
        assertEquals("6f1c2b9e-0000-4000-8000-000000000001", reg?.providerId)
        assertEquals("eyJ.abc.def", reg?.token)
    }

    @Test
    fun brokenOrIncompleteRegistrationIsNull() {
        assertNull(ProviderAccountParser.registration("not json"))
        assertNull(ProviderAccountParser.registration("""{"provider_id":"x"}"""))
    }

    @Test
    fun parsesStatusWithNullScores() {
        val s = ProviderAccountParser.status(
            """{"provider_id":"p","status":"ACTIVE","region":"","asn":"","consecutive_audit_passes":7,
               "score_24h":null,"score_7d":null,"score_30d":null,"score_composite":null,
               "pending_earnings_paise":12345,"held_earnings_paise":0,"last_heartbeat_ts":"2026-11-06T10:00:00Z",
               "multiaddr_stale":false,"stored_chunks":42,"p95_throughput_kbps":0,"accelerated_reaudit":false,
               "first_chunk_assignment_at":null,"network_mode":"demo","storage_advisory_gb":10}""",
        )
        assertEquals("ACTIVE", s.status)
        assertEquals(12345L, s.pendingEarningsPaise)
        assertEquals(7, s.consecutiveAuditPasses)
        assertNull(s.scoreComposite)
        assertEquals("2026-11-06T10:00:00Z", s.lastHeartbeat)
        assertEquals(42, s.storedChunks)
        assertEquals("demo", s.networkMode)
    }

    @Test
    fun parsesStatusScore() {
        val s = ProviderAccountParser.status("""{"status":"VETTING","score_composite":0.875,"last_heartbeat_ts":null}""")
        assertEquals(0.875, s.scoreComposite!!, 1e-9)
        assertNull(s.lastHeartbeat)
    }

    @Test
    fun parsesReceiptsAndCountsResults() {
        val page = ProviderAccountParser.receipts(
            """{"receipts":[
                {"server_challenge_ts":"2026-11-06T10:02:00Z","audit_result":"PASS","response_latency_ms":40},
                {"server_challenge_ts":"2026-11-06T10:00:00Z","audit_result":"TIMEOUT","response_latency_ms":null},
                {"server_challenge_ts":"2026-11-06T09:58:00Z","audit_result":null,"response_latency_ms":null}
               ],"total":3,"next_cursor":null}""",
        )
        assertEquals(3, page.total)
        assertEquals(1, page.passed)
        assertEquals(1, page.failed)
        assertNull(page.receipts[1].latencyMs)
    }

    @Test
    fun errorCodeFromAnErrorBody() {
        assertEquals("PROVIDER_DEPARTED", ProviderAccountParser.errorCode("""{"error_code":"PROVIDER_DEPARTED","message":"x"}"""))
        assertNull(ProviderAccountParser.errorCode("<html>"))
    }
}
