package org.vyomanaut.provider.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AddressPickerTest {
    private val otherVpn = NetAddress("10.8.0.2", 24)
    private val zeroTier = NetAddress("172.27.40.17", 16)
    private val wifi = NetAddress("192.168.1.23", 24, isVpn = false)

    @Test
    fun picksTheAddressInTheCoordinatorsSubnet() {
        assertEquals(zeroTier, AddressPicker.pick(listOf(otherVpn, zeroTier), "172.27.1.5"))
    }

    @Test
    fun aCoordinatorOnTheWifiGetsTheWifiAddressEvenWithZeroTierOn() {
        assertEquals(wifi, AddressPicker.pick(listOf(zeroTier, wifi), "192.168.1.10"))
    }

    @Test
    fun fallsBackToTheFirstVpnAddressForAHostName() {
        assertEquals(otherVpn, AddressPicker.pick(listOf(wifi, otherVpn, zeroTier), "coordinator.example"))
    }

    @Test
    fun fallsBackToAnyAddressWhenThereIsNoVpn() {
        assertEquals(wifi, AddressPicker.pick(listOf(wifi), "10.99.0.1"))
    }

    @Test
    fun noAddressesMeansNone() {
        assertNull(AddressPicker.pick(emptyList(), "172.27.1.5"))
    }

    @Test
    fun badPrefixLengthsNeverMatch() {
        assertEquals(
            otherVpn,
            AddressPicker.pick(listOf(otherVpn, NetAddress("172.27.40.17", 0)), "172.27.1.5"),
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
