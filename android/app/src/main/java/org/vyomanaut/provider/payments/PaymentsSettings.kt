package org.vyomanaut.provider.payments

import android.content.Context
import java.net.URI
import java.net.URISyntaxException

/** Pure defaults for where the Payments tab reads from. */
object PaymentsDefaults {
    /** The coordinator's chain API listens on its own port (VYOMANAUT_CHAIN_API_ADDR, default :8090). */
    const val CHAIN_API_PORT = 8090

    /** "http://172.27.1.5:8080" -> "http://172.27.1.5:8090"; null when the coordinator URL is unusable. */
    fun chainApiFor(coordinatorUrl: String): String? = try {
        val uri = URI(coordinatorUrl.trim())
        val host = uri.host
        if (uri.scheme == null || host.isNullOrEmpty()) null else "${uri.scheme}://$host:$CHAIN_API_PORT"
    } catch (e: URISyntaxException) {
        null
    }
}

/**
 * The chain API and RPC addresses the Payments tab uses. Empty means "use the default". The RPC URL can
 * carry a provider's API key, so it stays in private app storage and is never logged.
 */
class PaymentsSettings(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun savedChainApiUrl(): String = prefs.getString(KEY_CHAIN_API, "") ?: ""

    fun savedRpcUrl(): String = prefs.getString(KEY_RPC, "") ?: ""

    fun chainApiUrl(coordinatorUrl: String): String? =
        savedChainApiUrl().ifBlank { null } ?: PaymentsDefaults.chainApiFor(coordinatorUrl)

    fun rpcUrl(): String = savedRpcUrl().ifBlank { CoordinatorWallet.DEFAULT_RPC_URL }

    fun save(chainApiUrl: String, rpcUrl: String) {
        prefs.edit()
            .putString(KEY_CHAIN_API, chainApiUrl.trim().trimEnd('/'))
            .putString(KEY_RPC, rpcUrl.trim())
            .apply()
    }

    private companion object {
        const val PREFS_NAME = "payments_settings"
        const val KEY_CHAIN_API = "chain_api_url"
        const val KEY_RPC = "rpc_url"
    }
}
