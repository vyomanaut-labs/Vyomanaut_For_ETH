package org.vyomanaut.provider.payments

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.math.BigInteger

class CoordinatorWalletTest {
    private val escrow = "0x" + "e".repeat(40)
    private val token = "0x" + "7".repeat(40)
    private val coordinator = "0x" + "c".repeat(40)

    private fun word(n: Long) = Abi.uintWord(BigInteger.valueOf(n))
    private fun addr(a: String) = Abi.addressWord(a)

    /** Answers exactly the calls CoordinatorWallet makes; anything else fails the test. */
    private class FakeChain(
        private val chain: Long,
        private val answers: Map<Pair<String, String>, String>,
        private val native: BigInteger,
    ) : EthReader {
        override fun chainId() = chain
        override fun balance(address: String) = native
        override fun call(to: String, data: String): String =
            answers[to to data] ?: throw AssertionError("unexpected call $data to $to")
    }

    private fun chain(lastPeriod: Long, chainId: Long = 1874): FakeChain {
        val symbol = "0x" + word(32) + word(4) + "55534454" + "0".repeat(56)
        val answers = mutableMapOf(
            (escrow to Abi.call(Abi.COORDINATOR)) to "0x" + addr(coordinator),
            (token to Abi.call(Abi.DECIMALS)) to "0x" + word(6),
            (token to Abi.call(Abi.SYMBOL)) to symbol,
            (escrow to Abi.call(Abi.LAST_PERIOD)) to "0x" + word(lastPeriod),
            (token to Abi.call(Abi.BALANCE_OF, addr(coordinator))) to "0x" + word(5_000_000),
            (token to Abi.call(Abi.BALANCE_OF, addr(escrow))) to "0x" + word(90_000_000),
            (escrow to Abi.call(Abi.TOTAL_CREDIT)) to "0x" + word(60_000_000),
            (escrow to Abi.call(Abi.TOTAL_UNCLAIMED)) to "0x" + word(30_000_000),
            (escrow to Abi.call(Abi.CHALLENGE_WINDOW)) to "0x" + word(600),
            (escrow to Abi.call(Abi.LAST_POST_AT)) to "0x" + word(1_790_000_000),
        )
        if (lastPeriod > 0) {
            answers[escrow to Abi.call(Abi.PERIOD_INFO, word(lastPeriod))] =
                "0x" + word(12_000_000) + word(4_000_000) + word(1_790_000_600) + word(0)
        }
        return FakeChain(chainId, answers, BigInteger.TEN.pow(18))
    }

    private val info = ChainInfo(chainId = 1874, escrow = escrow, token = token, anchor = null, explorer = null)

    @Test
    fun readsTheCoordinatorWalletAndEscrow() {
        val w = CoordinatorWallet.read(info, chain(lastPeriod = 3))
        assertEquals(coordinator, w.coordinator)
        assertEquals("USDT", w.tokenSymbol)
        assertEquals(6, w.tokenDecimals)
        assertEquals(BigInteger.valueOf(5_000_000), w.coordinatorTokenBalance)
        assertEquals(BigInteger.valueOf(30_000_000), w.totalUnclaimed)
        assertEquals(600L, w.challengeWindowSeconds)
        assertEquals(3L, w.lastPeriod?.period)
        assertEquals(BigInteger.valueOf(4_000_000), w.lastPeriod?.unclaimed)
        assertFalse(w.lastPeriod!!.vetoed)
        // Whitechain Sepolia gets its explorer even when the coordinator names none.
        assertEquals("https://explorer.testnet.whitechain.io/address/$coordinator", w.addressLink(coordinator))
    }

    @Test
    fun noPeriodPostedYet() {
        assertNull(CoordinatorWallet.read(info, chain(lastPeriod = 0)).lastPeriod)
    }

    @Test
    fun aNodeOnTheWrongChainIsRefused() {
        try {
            CoordinatorWallet.read(info, chain(lastPeriod = 0, chainId = 1))
            throw AssertionError("expected a chain mismatch")
        } catch (e: IOException) {
            assertTrue(e.message!!.contains("chain 1"))
        }
    }

    @Test
    fun parsesChainInfo() {
        val parsed = CoordinatorWallet.parseInfo(
            """{"chain_id":1874,"escrow":"$escrow","token":"$token","explorer":"https://x.example"}""",
        )
        assertEquals(1874L, parsed.chainId)
        assertEquals("https://x.example", parsed.explorer)
        assertNull(parsed.anchor)
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsABadContractAddress() {
        CoordinatorWallet.parseInfo("""{"chain_id":1874,"escrow":"0x12","token":"$token"}""")
    }
}
