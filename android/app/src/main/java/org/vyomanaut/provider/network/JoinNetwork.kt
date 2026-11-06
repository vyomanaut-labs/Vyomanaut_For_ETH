package org.vyomanaut.provider.network

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.vyomanaut.provider.payments.Http
import org.vyomanaut.provider.payments.PaymentsSettings
import java.io.IOException

/** Saves a network to join, and asks a coordinator where its relay and chain API are. */
object JoinNetwork {
    /** Saves [link] as this phone's network, with [phone] as its number. */
    fun apply(context: Context, link: JoinLink, phone: String) {
        val store = OnboardingStore(context)
        store.save(link.coordinatorUrl, phone)
        store.saveRelay(link.relay ?: "")
        store.saveNetworkName(link.networkName)
        val payments = PaymentsSettings(context)
        payments.save(link.chainApiUrl ?: "", payments.savedRpcUrl())
    }

    /** The coordinator's own description of its network, or null when it has none (an older coordinator). */
    suspend fun fetchInfo(coordinatorUrl: String): JoinLink? = withContext(Dispatchers.IO) {
        try {
            val reply = Http.get(coordinatorUrl + JoinLinks.INFO_PATH)
            if (reply.ok) JoinLinks.fromInfo(reply.body) else null
        } catch (e: IOException) {
            null
        }
    }
}
