package org.vyomanaut.provider.network

import org.json.JSONException
import org.json.JSONObject
import java.net.URI
import java.net.URISyntaxException
import java.net.URLDecoder

/**
 * A network to join, from the coordinator's join page (`vyomanaut://join?coordinator=…&relay=…&chain=…&name=…`)
 * or from its `GET /api/v1/network/info`.
 */
data class JoinLink(
    val coordinatorUrl: String,
    /** First usable relay, "host:port"; null = the default (coordinator host :30300). */
    val relay: String?,
    /** The chain API for the Payments tab; null = the default. */
    val chainApiUrl: String?,
    val networkName: String,
)

/** Pure parsing, unit-tested on the host. */
object JoinLinks {
    const val SCHEME = "vyomanaut"
    const val HOST = "join"
    const val INFO_PATH = "/api/v1/network/info"
    const val DEFAULT_NAME = "Vyomanaut network"
    private const val MAX_NAME = 60

    /** Null when [link] is not a usable join link. */
    fun parse(link: String): JoinLink? {
        val uri = try {
            URI(link.trim())
        } catch (e: URISyntaxException) {
            return null
        }
        if (!SCHEME.equals(uri.scheme, ignoreCase = true) || !HOST.equals(uri.host, ignoreCase = true)) return null
        val q = query(uri.rawQuery ?: return null)
        return build(q["coordinator"], q["relay"]?.split(','), q["chain"], q["name"])
    }

    /** Reads `GET /api/v1/network/info`. Null when [json] is not one. */
    fun fromInfo(json: String): JoinLink? = try {
        val o = JSONObject(json)
        val relays = o.optJSONArray("relay_addrs")?.let { a -> (0 until a.length()).map { a.optString(it) } }
        build(o.optString("coordinator_url"), relays, o.optString("chain_api_url"), o.optString("network_name"))
    } catch (e: JSONException) {
        null
    }

    private fun build(coordinator: String?, relays: List<String>?, chain: String?, name: String?): JoinLink? {
        val url = NetworkRules.normalizeBaseUrl(coordinator ?: "") ?: return null
        val relay = relays.orEmpty().map { it.trim() }
            .firstOrNull { it.isNotEmpty() && !it.equals(RelayRules.OFF, true) && RelayRules.problemWith(it) == null }
        val chainUrl = chain?.takeIf { it.isNotBlank() }?.let { NetworkRules.normalizeBaseUrl(it) }
        val cleanName = name.orEmpty().filter { it.isLetterOrDigit() || it in " .,&()_+-" }.trim().take(MAX_NAME)
        return JoinLink(url, relay, chainUrl, cleanName.ifEmpty { DEFAULT_NAME })
    }

    private fun query(raw: String): Map<String, String> = raw.split('&').mapNotNull { pair ->
        val i = pair.indexOf('=')
        if (i <= 0) {
            null
        } else {
            try {
                URLDecoder.decode(pair.substring(0, i), "UTF-8") to URLDecoder.decode(pair.substring(i + 1), "UTF-8")
            } catch (e: IllegalArgumentException) {
                null
            }
        }
    }.toMap()
}
