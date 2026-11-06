package org.vyomanaut.provider.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NetworkRulesTest {

    @Test
    fun url_strips_trailing_slash() {
        assertEquals("http://10.35.114.52:8080", NetworkRules.normalizeBaseUrl(" http://10.35.114.52:8080/ "))
    }

    @Test
    fun url_accepts_https() {
        assertEquals("https://coord.example.in", NetworkRules.normalizeBaseUrl("https://coord.example.in"))
    }

    @Test
    fun url_without_scheme_is_rejected() {
        assertNull(NetworkRules.normalizeBaseUrl("10.35.114.52:8080"))
    }

    @Test
    fun url_with_other_scheme_is_rejected() {
        assertNull(NetworkRules.normalizeBaseUrl("ftp://10.35.114.52"))
    }

    @Test
    fun url_without_host_is_rejected() {
        assertNull(NetworkRules.normalizeBaseUrl("http://"))
    }

    @Test
    fun phone_separators_are_removed() {
        assertEquals("+919876543210", NetworkRules.normalizePhone("+91 98765-43210"))
    }

    @Test
    fun phone_too_short_is_rejected() {
        assertNull(NetworkRules.normalizePhone("12345"))
    }

    @Test
    fun phone_with_letters_is_rejected() {
        assertNull(NetworkRules.normalizePhone("98765abcde"))
    }

    @Test
    fun skew_is_positive_when_phone_is_ahead() {
        assertEquals(12L, NetworkRules.clockSkewSeconds(1_000_012L, 1_000_000L))
    }

    @Test
    fun skew_limit_is_thirty_seconds_either_way() {
        assertFalse(NetworkRules.isClockSkewTooLarge(30L))
        assertTrue(NetworkRules.isClockSkewTooLarge(31L))
        assertTrue(NetworkRules.isClockSkewTooLarge(-31L))
    }
}
