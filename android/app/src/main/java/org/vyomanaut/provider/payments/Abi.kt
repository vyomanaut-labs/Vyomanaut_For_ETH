package org.vyomanaut.provider.payments

import java.math.BigInteger

/**
 * The few Solidity ABI pieces the app needs for read-only `eth_call`s, so the app does not pull in a full
 * Ethereum library. Selectors are the first 4 bytes of keccak256 of the function signature; they were
 * computed with go-ethereum's crypto.Keccak256 and match contracts/src/VyomaEscrow.sol and ERC-20.
 */
object Abi {
    // VyomaEscrow public views.
    const val COORDINATOR = "0a009097" // coordinator()
    const val TOKEN = "fc0c546a" // token()
    const val TOTAL_CREDIT = "a43d6404" // totalCredit()
    const val TOTAL_UNCLAIMED = "c96f14b8" // totalUnclaimed()
    const val LAST_PERIOD = "d340ef8a" // lastPeriod()
    const val LAST_POST_AT = "7b3413fc" // lastPostAt()
    const val CHALLENGE_WINDOW = "861a1412" // challengeWindow()
    const val PERIOD_INFO = "b40b63d8" // periodInfo(uint64)

    // ERC-20.
    const val BALANCE_OF = "70a08231" // balanceOf(address)
    const val DECIMALS = "313ce567" // decimals()
    const val SYMBOL = "95d89b41" // symbol()

    private const val WORD_HEX = 64
    private const val ADDRESS_HEX = 40
    private val addressFormat = Regex("^0x[0-9a-fA-F]{40}$")

    fun isAddress(text: String): Boolean = addressFormat.matches(text)

    /** "0x" + selector + one 32-byte word per argument. */
    fun call(selector: String, vararg words: String): String = "0x" + selector + words.joinToString("")

    fun addressWord(address: String): String {
        require(isAddress(address)) { "not an address: $address" }
        return address.substring(2).lowercase().padStart(WORD_HEX, '0')
    }

    fun uintWord(value: BigInteger): String {
        require(value.signum() >= 0) { "negative uint" }
        return value.toString(16).padStart(WORD_HEX, '0')
    }

    /** The [index]-th 32-byte word of a call result, as an unsigned integer. */
    fun uintAt(result: String, index: Int = 0): BigInteger = BigInteger(word(result, index), 16)

    /** The [index]-th word as an address ("0x" + the low 20 bytes, lower case). */
    fun addressAt(result: String, index: Int = 0): String = "0x" + word(result, index).takeLast(ADDRESS_HEX)

    fun boolAt(result: String, index: Int = 0): Boolean = uintAt(result, index).signum() != 0

    /** Decodes an ABI-encoded `string` return value (offset, length, bytes). */
    fun stringResult(result: String): String {
        val offsetWords = uintAt(result, 0).toInt() / (WORD_HEX / 2)
        val length = uintAt(result, offsetWords).toInt()
        val hex = strip(result)
        val start = (offsetWords + 1) * WORD_HEX
        require(hex.length >= start + length * 2) { "string result too short" }
        val bytes = ByteArray(length) { i -> hex.substring(start + i * 2, start + i * 2 + 2).toInt(16).toByte() }
        return String(bytes, Charsets.UTF_8)
    }

    private fun word(result: String, index: Int): String {
        val hex = strip(result)
        val start = index * WORD_HEX
        require(index >= 0 && hex.length >= start + WORD_HEX) { "call result has no word $index" }
        return hex.substring(start, start + WORD_HEX)
    }

    private fun strip(result: String): String = result.removePrefix("0x").removePrefix("0X")
}
