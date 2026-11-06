package org.vyomanaut.provider.payments

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.math.BigInteger

/** The read-only chain operations the coordinator-wallet view needs. An interface so tests can fake it. */
interface EthReader {
    @Throws(IOException::class)
    fun chainId(): Long

    @Throws(IOException::class)
    fun balance(address: String): BigInteger

    /** eth_call against the latest block; returns the raw hex result. */
    @Throws(IOException::class)
    fun call(to: String, data: String): String
}

/**
 * JSON-RPC over HTTP to an EVM node. The URL may carry an API key, so it is never logged or shown;
 * error messages mention only what failed.
 */
class EvmRpc(private val rpcUrl: String) : EthReader {
    private var nextId = 1

    override fun chainId(): Long = hexToBig(request("eth_chainId", JSONArray())).toLong()

    override fun balance(address: String): BigInteger =
        hexToBig(request("eth_getBalance", JSONArray().put(address).put(LATEST)))

    override fun call(to: String, data: String): String =
        request("eth_call", JSONArray().put(JSONObject().put("to", to).put("data", data)).put(LATEST))

    private fun request(method: String, params: JSONArray): String {
        val body = JSONObject()
            .put("jsonrpc", "2.0")
            .put("id", nextId++)
            .put("method", method)
            .put("params", params)
            .toString()
        val reply = Http.postJson(rpcUrl, body)
        if (!reply.ok) throw IOException("chain node answered HTTP ${reply.code} to $method")
        return try {
            val o = JSONObject(reply.body)
            o.optJSONObject("error")?.let { throw IOException("chain node refused $method: ${it.optString("message")}") }
            o.getString("result")
        } catch (e: JSONException) {
            throw IOException("chain node sent an unexpected reply to $method")
        }
    }

    private fun hexToBig(hex: String): BigInteger {
        val digits = hex.removePrefix("0x")
        return if (digits.isEmpty()) BigInteger.ZERO else BigInteger(digits, HEX)
    }

    private companion object {
        const val LATEST = "latest"
        const val HEX = 16
    }
}
