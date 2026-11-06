package org.vyomanaut.provider.payments

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigInteger

class MoneyAbiTest {

    @Test
    fun paiseFormatting() {
        assertEquals("₹0.00", Money.formatPaise(0))
        assertEquals("₹0.05", Money.formatPaise(5))
        assertEquals("₹1,234.56", Money.formatPaise(123_456))
        assertEquals("-₹12.30", Money.formatPaise(-1_230))
    }

    @Test
    fun unitsRoundDownAndGroup() {
        assertEquals("1.2345", Money.formatUnits(BigInteger.valueOf(1_234_567), 6))
        assertEquals("1,000", Money.formatUnits(BigInteger.valueOf(1_000_000_000), 6))
        assertEquals("0", Money.formatUnits(BigInteger.ZERO, 18))
        assertEquals("0.5", Money.formatUnits(BigInteger.TEN.pow(17).multiply(BigInteger.valueOf(5)), 18))
    }

    @Test
    fun shortAddress() {
        assertEquals("0x1234…abcd", Money.shortAddress("0x1234567890123456789012345678901234abcd"))
        assertEquals("0x12", Money.shortAddress("0x12"))
    }

    @Test
    fun encodesBalanceOfLikeSolidity() {
        val call = Abi.call(Abi.BALANCE_OF, Abi.addressWord("0x00000000000000000000000000000000000000AB"))
        assertEquals("0x70a08231" + "0".repeat(62) + "ab", call)
    }

    @Test
    fun encodesUintArguments() {
        assertEquals("0".repeat(62) + "2a", Abi.uintWord(BigInteger.valueOf(42)))
    }

    @Test
    fun decodesWordsAddressesAndBools() {
        val result = "0x" +
            "0".repeat(24) + "1111111111111111111111111111111111111111" +
            "0".repeat(63) + "1" +
            "0".repeat(62) + "ff"
        assertEquals("0x1111111111111111111111111111111111111111", Abi.addressAt(result, 0))
        assertTrue(Abi.boolAt(result, 1))
        assertEquals(BigInteger.valueOf(255), Abi.uintAt(result, 2))
    }

    @Test
    fun decodesAStringResult() {
        // symbol() returning "USDT": offset 0x20, length 4, then the bytes padded to a word.
        val result = "0x" +
            "0".repeat(62) + "20" +
            "0".repeat(63) + "4" +
            "55534454" + "0".repeat(56)
        assertEquals("USDT", Abi.stringResult(result))
    }

    @Test
    fun addressValidation() {
        assertTrue(Abi.isAddress("0x" + "a".repeat(40)))
        assertFalse(Abi.isAddress("0x" + "a".repeat(39)))
        assertFalse(Abi.isAddress("a".repeat(42)))
    }
}
