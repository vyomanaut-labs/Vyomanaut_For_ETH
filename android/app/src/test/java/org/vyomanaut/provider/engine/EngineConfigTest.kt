package org.vyomanaut.provider.engine

import org.vyomanaut.provider.storage.BYTES_PER_GIB
import org.vyomanaut.provider.storage.BYTES_PER_MIB
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EngineConfigTest {

    private val config = EngineConfig(
        coordinatorUrl = "http://10.35.114.52:8080",
        phone = "+919876543210",
        storageGb = 2,
        listenPort = 30303,
        advertiseAddr = "10.35.114.30",
        dataDir = "/data/provider",
    )

    @Test
    fun storageGb_rounds_down_to_whole_gib() {
        assertEquals(2, ProviderArgs.storageGbFromLimit(2 * BYTES_PER_GIB + 600 * BYTES_PER_MIB))
    }

    @Test
    fun storageGb_is_zero_below_one_gib() {
        assertEquals(0, ProviderArgs.storageGbFromLimit(BYTES_PER_GIB - 1))
        assertEquals(0, ProviderArgs.storageGbFromLimit(0L))
    }

    @Test
    fun onboard_args_match_the_windows_installer_flags() {
        assertEquals(
            listOf(
                "onboard",
                "--microservice-url=http://10.35.114.52:8080",
                "--phone=+919876543210",
                "--storage-gb=2",
                "--data-dir=/data/provider",
                "--listen-port=30303",
                "--advertise-addr=10.35.114.30",
            ),
            ProviderArgs.onboard(config),
        )
    }

    @Test
    fun run_args_use_demo_mode_and_declared_storage() {
        assertEquals(
            listOf(
                "run",
                "--mode=demo",
                "--microservice-url=http://10.35.114.52:8080",
                "--data-dir=/data/provider",
                "--declared-storage-gb=2",
                "--listen-port=30303",
                "--advertise-addr=10.35.114.30",
            ),
            ProviderArgs.run(config),
        )
    }

    @Test
    fun otp_must_be_exactly_six_digits() {
        assertTrue(ProviderArgs.isValidOtp("123456"))
        assertFalse(ProviderArgs.isValidOtp("12345"))
        assertFalse(ProviderArgs.isValidOtp("1234567"))
        assertFalse(ProviderArgs.isValidOtp("12345a"))
    }

    @Test
    fun builder_reports_missing_coordinator() {
        val result = EngineConfigBuilder.build("", "+919876543210", 3 * BYTES_PER_GIB, listOf("10.0.0.2"), "/d")
        assertTrue(result is ConfigResult.Missing)
    }

    @Test
    fun builder_reports_limit_below_one_gb() {
        val result = EngineConfigBuilder.build("http://h:1", "+919876543210", BYTES_PER_GIB / 2, listOf("10.0.0.2"), "/d")
        assertTrue(result is ConfigResult.Missing)
    }

    @Test
    fun builder_reports_zerotier_off() {
        val result = EngineConfigBuilder.build("http://h:1", "+919876543210", 3 * BYTES_PER_GIB, emptyList(), "/d")
        assertTrue(result is ConfigResult.Missing)
    }

    @Test
    fun builder_is_ready_with_everything_set() {
        val result = EngineConfigBuilder.build(
            "http://h:1", "+919876543210", 3 * BYTES_PER_GIB, listOf("10.35.114.30", "10.9.9.9"), "/d",
        )
        val ready = result as ConfigResult.Ready
        // A 3 GB limit is below the coordinator minimum, so 10 GB is declared.
        assertEquals(10, ready.config.storageGb)
        assertEquals("10.35.114.30", ready.config.advertiseAddr)
        assertEquals(30303, ready.config.listenPort)
    }

    @Test
    fun builder_declares_the_coordinator_minimum_for_a_small_limit() {
        val result = EngineConfigBuilder.build("http://h:1", "+919876543210", 2 * BYTES_PER_GIB + 600 * BYTES_PER_MIB, listOf("10.0.0.2"), "/d")
        assertEquals(10, (result as ConfigResult.Ready).config.storageGb)
    }

    @Test
    fun builder_declares_the_limit_itself_when_above_the_minimum() {
        val result = EngineConfigBuilder.build("http://h:1", "+919876543210", 20 * BYTES_PER_GIB, listOf("10.0.0.2"), "/d")
        assertEquals(20, (result as ConfigResult.Ready).config.storageGb)
    }
}
