package org.vyomanaut.provider.network

import android.os.SystemClock
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

sealed interface CheckResult {
    data class Reachable(val latencyMs: Long, val skewSeconds: Long) : CheckResult
    data class Failed(val reason: String) : CheckResult
}

/** Calls the coordinator's GET /api/v1/time: proves the ZeroTier path works and measures clock offset. */
object CoordinatorCheck {
    private const val TIME_PATH = "/api/v1/time"
    private const val TIMEOUT_MS = 5_000
    private const val HTTP_OK = 200
    private const val MS_PER_SECOND = 1_000L

    suspend fun check(baseUrl: String): CheckResult = withContext(Dispatchers.IO) {
        val started = SystemClock.elapsedRealtime()
        var connection: HttpURLConnection? = null
        try {
            connection = (URL(baseUrl + TIME_PATH).openConnection() as HttpURLConnection).apply {
                connectTimeout = TIMEOUT_MS
                readTimeout = TIMEOUT_MS
                requestMethod = "GET"
            }
            val code = connection.responseCode
            if (code != HTTP_OK) {
                return@withContext CheckResult.Failed("Coordinator answered HTTP $code")
            }
            val body = connection.inputStream.bufferedReader().use { it.readText() }
            val latency = SystemClock.elapsedRealtime() - started
            val serverEpoch = JSONObject(body).getLong("unix_epoch")
            val phoneEpoch = System.currentTimeMillis() / MS_PER_SECOND
            CheckResult.Reachable(latency, NetworkRules.clockSkewSeconds(phoneEpoch, serverEpoch))
        } catch (e: IOException) {
            CheckResult.Failed("Cannot reach the coordinator: ${e.message}")
        } catch (e: JSONException) {
            CheckResult.Failed("Unexpected reply. This is not a Vyomanaut coordinator address.")
        } finally {
            connection?.disconnect()
        }
    }
}
