package org.vyomanaut.provider.payments

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.math.BigInteger

/** GET /chain/v1/info from the coordinator's chain API: which contracts it settles through. */
data class ChainInfo(
    val chainId: Long,
    val escrow: String,
    val token: String,
    val anchor: String?,
    val explorer: String?,
)

/** The latest payout period the coordinator wallet posted to the escrow. */
data class PeriodSnapshot(
    val period: Long,
    val total: BigInteger,
    val unclaimed: BigInteger,
    /** Unix seconds after which the posted amounts can be claimed (the end of the challenge window). */
    val claimableAt: Long,
    val vetoed: Boolean,
)

/** Everything the Payments tab shows about the coordinator wallet that decides and pays out earnings. */
data class WalletSnapshot(
    val chainId: Long,
    val coordinator: String,
    val nativeBalanceWei: BigInteger,
    val token: String,
    val tokenSymbol: String,
    val tokenDecimals: Int,
    val coordinatorTokenBalance: BigInteger,
    val escrow: String,
    val escrowTokenBalance: BigInteger,
    val totalCredit: BigInteger,
    val totalUnclaimed: BigInteger,
    val challengeWindowSeconds: Long,
    val lastPostAt: Long,
    val lastPeriod: PeriodSnapshot?,
    val explorer: String?,
) {
    fun addressLink(address: String): String? = explorer?.trimEnd('/')?.let { "$it/address/$address" }
}

/**
 * Reads the coordinator wallet straight from the chain, not from the coordinator: the coordinator only
 * names the escrow contract (/chain/v1/info), and every number shown is read from that contract and its
 * token over JSON-RPC, so anyone can check it independently.
 */
object CoordinatorWallet {
    /** Whitechain Sepolia, the chain docs/WHITECHAIN.md targets. Used when the coordinator names no explorer. */
    const val DEFAULT_RPC_URL = "https://rpc.testnet.whitechain.io"
    private const val WHITECHAIN_SEPOLIA_ID = 1874L
    private const val WHITECHAIN_SEPOLIA_EXPLORER = "https://explorer.testnet.whitechain.io"
    const val NATIVE_DECIMALS = 18

    @Throws(JSONException::class)
    fun parseInfo(json: String): ChainInfo {
        val o = JSONObject(json)
        val escrow = o.getString("escrow")
        val token = o.getString("token")
        require(Abi.isAddress(escrow) && Abi.isAddress(token)) { "bad contract address in /chain/v1/info" }
        return ChainInfo(
            chainId = o.getLong("chain_id"),
            escrow = escrow,
            token = token,
            anchor = o.optString("anchor").ifBlank { null },
            explorer = o.optString("explorer").ifBlank { null },
        )
    }

    /** Reads every value; blocking, so call it off the main thread (see [load]). */
    @Throws(IOException::class)
    fun read(info: ChainInfo, eth: EthReader): WalletSnapshot {
        val nodeChain = eth.chainId()
        if (nodeChain != info.chainId) {
            throw IOException("the chain node is on chain $nodeChain but the coordinator settles on chain ${info.chainId}")
        }
        val escrow = info.escrow
        val coordinator = Abi.addressAt(eth.call(escrow, Abi.call(Abi.COORDINATOR)))
        val decimals = Abi.uintAt(eth.call(info.token, Abi.call(Abi.DECIMALS))).toInt()
        val symbol = try {
            Abi.stringResult(eth.call(info.token, Abi.call(Abi.SYMBOL)))
        } catch (e: IllegalArgumentException) {
            "tokens"
        }
        val lastPeriodNumber = Abi.uintAt(eth.call(escrow, Abi.call(Abi.LAST_PERIOD))).toLong()
        val lastPeriod = if (lastPeriodNumber == 0L) {
            null
        } else {
            val r = eth.call(escrow, Abi.call(Abi.PERIOD_INFO, Abi.uintWord(BigInteger.valueOf(lastPeriodNumber))))
            PeriodSnapshot(
                period = lastPeriodNumber,
                total = Abi.uintAt(r, 0),
                unclaimed = Abi.uintAt(r, 1),
                claimableAt = Abi.uintAt(r, 2).toLong(),
                vetoed = Abi.boolAt(r, 3),
            )
        }
        return WalletSnapshot(
            chainId = nodeChain,
            coordinator = coordinator,
            nativeBalanceWei = eth.balance(coordinator),
            token = info.token,
            tokenSymbol = symbol,
            tokenDecimals = decimals,
            coordinatorTokenBalance = Abi.uintAt(eth.call(info.token, Abi.call(Abi.BALANCE_OF, Abi.addressWord(coordinator)))),
            escrow = escrow,
            escrowTokenBalance = Abi.uintAt(eth.call(info.token, Abi.call(Abi.BALANCE_OF, Abi.addressWord(escrow)))),
            totalCredit = Abi.uintAt(eth.call(escrow, Abi.call(Abi.TOTAL_CREDIT))),
            totalUnclaimed = Abi.uintAt(eth.call(escrow, Abi.call(Abi.TOTAL_UNCLAIMED))),
            challengeWindowSeconds = Abi.uintAt(eth.call(escrow, Abi.call(Abi.CHALLENGE_WINDOW))).toLong(),
            lastPostAt = Abi.uintAt(eth.call(escrow, Abi.call(Abi.LAST_POST_AT))).toLong(),
            lastPeriod = lastPeriod,
            explorer = info.explorer ?: WHITECHAIN_SEPOLIA_EXPLORER.takeIf { nodeChain == WHITECHAIN_SEPOLIA_ID },
        )
    }

    suspend fun load(chainApiUrl: String, rpcUrl: String): Fetch<WalletSnapshot> = withContext(Dispatchers.IO) {
        val info = try {
            val reply = Http.get("$chainApiUrl/chain/v1/info")
            if (!reply.ok) {
                return@withContext Fetch.Error(
                    "The coordinator's chain API answered HTTP ${reply.code}. Is on-chain settlement turned on?",
                )
            }
            parseInfo(reply.body)
        } catch (e: IOException) {
            return@withContext Fetch.Error(
                "Cannot reach the coordinator's chain API (on-chain settlement may be off): ${e.message}",
            )
        } catch (e: JSONException) {
            return@withContext Fetch.Error("Unexpected reply from the chain API.")
        } catch (e: IllegalArgumentException) {
            return@withContext Fetch.Error("The chain API named an invalid contract address.")
        }
        try {
            Fetch.Ok(read(info, EvmRpc(rpcUrl)))
        } catch (e: IOException) {
            Fetch.Error("Cannot read the chain: ${e.message}")
        } catch (e: IllegalArgumentException) {
            Fetch.Error("The chain returned data the app does not understand.")
        }
    }
}
