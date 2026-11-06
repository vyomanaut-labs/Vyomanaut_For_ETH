package org.vyomanaut.provider.payments

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONException
import org.vyomanaut.provider.engine.EngineConfigs
import java.io.File
import java.io.IOException

/** Either a value or a message to show the user. */
sealed interface Fetch<out T> {
    data class Ok<T>(val value: T) : Fetch<T>
    data class Error(val message: String) : Fetch<Nothing>
}

/**
 * Reads this provider's own earnings and audits from the coordinator, as the provider: it uses the bearer
 * token `provider onboard` saved in registration.json (GET routes need no request signature). The token is
 * never logged or shown.
 */
object CoordinatorAccountApi {
    private const val REGISTRATION_FILE = "registration.json"
    private const val RECENT_RECEIPTS = 20
    private const val HTTP_UNAUTHORIZED = 401
    private const val HTTP_NOT_FOUND = 404

    const val MSG_NOT_REGISTERED = "Register this phone on the Home tab to see its earnings."
    const val MSG_TOKEN_REJECTED =
        "The coordinator rejected this phone's login token. It lasts 7 days and the provider does not renew it " +
            "yet, or the coordinator was restarted. Use Reset registration on the Home tab to register again."

    fun registration(context: Context): Registration? {
        val file = File(EngineConfigs.dataDir(context), REGISTRATION_FILE)
        return try {
            if (file.exists()) ProviderAccountParser.registration(file.readText()) else null
        } catch (e: IOException) {
            null
        }
    }

    suspend fun status(baseUrl: String, registration: Registration): Fetch<ProviderStatus> =
        fetch("$baseUrl/api/v1/provider/${registration.providerId}/status", registration, ProviderAccountParser::status)

    suspend fun receipts(baseUrl: String, registration: Registration): Fetch<ReceiptPage> =
        fetch("$baseUrl/api/v1/provider/receipts?limit=$RECENT_RECEIPTS", registration, ProviderAccountParser::receipts)

    private suspend fun <T> fetch(url: String, registration: Registration, parse: (String) -> T): Fetch<T> =
        withContext(Dispatchers.IO) {
            try {
                val reply = Http.get(url, mapOf("Authorization" to "Bearer ${registration.token}"))
                when {
                    reply.ok -> Fetch.Ok(parse(reply.body))
                    reply.code == HTTP_UNAUTHORIZED -> Fetch.Error(MSG_TOKEN_REJECTED)
                    reply.code == HTTP_NOT_FOUND -> Fetch.Error("The coordinator does not know this provider. Register again.")
                    else -> Fetch.Error(
                        "Coordinator answered HTTP ${reply.code}" +
                            (ProviderAccountParser.errorCode(reply.body)?.let { " ($it)" } ?: ""),
                    )
                }
            } catch (e: IOException) {
                Fetch.Error("Cannot reach the coordinator: ${e.message}")
            } catch (e: JSONException) {
                Fetch.Error("Unexpected reply from the coordinator.")
            }
        }
}
