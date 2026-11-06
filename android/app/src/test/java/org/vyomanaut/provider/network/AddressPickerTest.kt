package org.vyomanaut.provider.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AddressPickerTest {
    private val otherVpn = VpnAddress("10.8.0.2", 24)
    private val zeroTier = VpnAddress("172.27.40.17", 16)

    @Test
    fun picksTheAddressInTheCoordinatorsSubnet() {
        assertEquals("172.27.40.17", AddressPicker.pick(listOf(otherVpn, zeroTier), "172.27.1.5"))
    }

    @Test
    fun fallsBackToTheFirstAddressForAHostName() {
        assertEquals("10.8.0.2", AddressPicker.pick(listOf(otherVpn, zeroTier), "coordinator.example"))
    }

    @Test
    fun fallsBackToTheFirstAddressWhenNoSubnetMatches() {
        assertEquals("10.8.0.2", AddressPicker.pick(listOf(otherVpn, zeroTier), "192.168.1.10"))
    }

    @Test
    fun noAddressesMeansNone() {
        assertNull(AddressPicker.pick(emptyList(), "172.27.1.5"))
    }

    @Test
    fun badPrefixLengthsNeverMatch() {
        assertEquals(
            "10.8.0.2",
            AddressPicker.pick(listOf(otherVpn, VpnAddress("172.27.40.17", 0)), "172.27.1.5"),
        )
    }

    @Test
    fun parsesOnlyRealIpv4Literals() {
        assertEquals(0xAC1B2811L, AddressPicker.parseIpv4("172.27.40.17"))
        assertNull(AddressPicker.parseIpv4("172.27.40"))
        assertNull(AddressPicker.parseIpv4("172.27.40.256"))
        assertNull(AddressPicker.parseIpv4("fe80::1"))
        assertNull(AddressPicker.parseIpv4("a.b.c.d"))
    }
}
