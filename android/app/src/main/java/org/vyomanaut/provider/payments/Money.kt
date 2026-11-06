package org.vyomanaut.provider.payments

import java.math.BigDecimal
import java.math.BigInteger
import java.math.RoundingMode

/** Pure money formatting. Integer arithmetic only, like the coordinator's ledger (no floating point). */
object Money {
    private const val PAISE_PER_RUPEE = 100L
    private const val SHORT_ADDRESS_HEAD = 6
    private const val SHORT_ADDRESS_TAIL = 4

    /** 123456 paise -> "₹1,234.56". */
    fun formatPaise(paise: Long): String {
        val sign = if (paise < 0) "-" else ""
        val abs = if (paise == Long.MIN_VALUE) Long.MAX_VALUE else kotlin.math.abs(paise)
        val rupees = abs / PAISE_PER_RUPEE
        val rest = abs % PAISE_PER_RUPEE
        return "$sign₹${groupThousands(rupees.toString())}.${rest.toString().padStart(2, '0')}"
    }

    /**
     * A token amount in its smallest unit -> a decimal string with at most [maxFraction] fraction digits,
     * rounded down so the app never shows more than is really there. 1_234_567 with 6 decimals -> "1.2345".
     */
    fun formatUnits(amount: BigInteger, decimals: Int, maxFraction: Int = 4): String {
        val value = BigDecimal(amount, decimals).setScale(maxFraction.coerceAtMost(decimals), RoundingMode.DOWN)
        val plain = value.stripTrailingZeros().toPlainString()
        val negative = plain.startsWith("-")
        val digits = plain.removePrefix("-")
        val whole = digits.substringBefore('.')
        val fraction = digits.substringAfter('.', "")
        val grouped = groupThousands(whole) + if (fraction.isEmpty()) "" else ".$fraction"
        return if (negative) "-$grouped" else grouped
    }

    /** "0x1234567890abcdef..." -> "0x1234…cdef". */
    fun shortAddress(address: String): String =
        if (address.length <= SHORT_ADDRESS_HEAD + SHORT_ADDRESS_TAIL + 1) address
        else address.take(SHORT_ADDRESS_HEAD) + "…" + address.takeLast(SHORT_ADDRESS_TAIL)

    private fun groupThousands(digits: String): String =
        digits.reversed().chunked(3).joinToString(",").reversed()
}
