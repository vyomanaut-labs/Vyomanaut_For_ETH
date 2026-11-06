package org.vyomanaut.provider.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import org.vyomanaut.provider.engine.EngineConfig
import org.vyomanaut.provider.engine.ProviderArgs

class RelayRulesTest {

    @Test
    fun defaultIsTheCoordinatorHostOnTheRelayPort() {
        assertEquals("10.35.114.52:30300", RelayRules.effectiveRelay("", "http://10.35.114.52:8080"))
    }

    @Test
    fun savedRelayWinsAndOffDisables() {
        assertEquals("10.0.0.9:31000", RelayRules.effectiveRelay(" 10.0.0.9:31000 ", "http://10.35.114.52:8080"))
        assertNull(RelayRules.effectiveRelay("OFF", "http://10.35.114.52:8080"))
        assertNull(RelayRules.effectiveRelay("", ""))
    }

    @Test
    fun validation() {
        assertNull(RelayRules.problemWith(""))
        assertNull(RelayRules.problemWith("off"))
        assertNull(RelayRules.problemWith("10.35.114.52:30300"))
        assertNotNull(RelayRules.problemWith("10.35.114.52"))
        assertNotNull(RelayRules.problemWith("http://10.35.114.52:30300"))
        assertNotNull(RelayRules.problemWith("10.35.114.52:70000"))
    }

    private val base = EngineConfig("http://c:8080", "+911234567890", 10, 30303, "10.35.114.7", "/data/p")

    @Test
    fun runArgumentsCarryTheRelay() {
        val args = ProviderArgs.run(base.copy(relayAddr = "10.35.114.52:30300"))
        assertEquals("--relay-addrs=10.35.114.52:30300", args[3])
    }

    @Test
    fun noRelayMeansNoFlag() {
        assert(ProviderArgs.run(base).none { it.startsWith("--relay-addrs") })
        assert(ProviderArgs.run(base.copy(relayAddr = " ")).none { it.startsWith("--relay-addrs") })
    }
}
