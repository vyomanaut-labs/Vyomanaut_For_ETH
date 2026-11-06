package org.vyomanaut.provider.payments

import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/** A finished HTTP exchange: the status code and the body (error body for non-2xx). */
data class HttpReply(val code: Int, val body: String) {
    val ok: Boolean get() = code in HTTP_OK_FIRST..HTTP_OK_LAST

    private companion object {
        const val HTTP_OK_FIRST = 200
        const val HTTP_OK_LAST = 299
    }
}

/** Small blocking HTTP helper for the Payments tab. Call it from a background dispatcher. */
object Http {
    private const val TIMEOUT_MS = 8_000

    @Throws(IOException::class)
    fun get(url: String, headers: Map<String, String> = emptyMap()): HttpReply = send("GET", url, headers, null)

    @Throws(IOException::class)
    fun postJson(url: String, body: String): HttpReply =
        send("POST", url, mapOf("Content-Type" to "application/json"), body)

    private fun send(method: String, url: String, headers: Map<String, String>, body: String?): HttpReply {
        val connection = URL(url).openConnection() as HttpURLConnection
        try {
            connection.connectTimeout = TIMEOUT_MS
            connection.readTimeout = TIMEOUT_MS
            connection.requestMethod = method
            headers.forEach { (k, v) -> connection.setRequestProperty(k, v) }
            if (body != null) {
                connection.doOutput = true
                connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
            }
            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader()?.use { it.readText() } ?: ""
            return HttpReply(code, text)
        } finally {
            connection.disconnect()
        }
    }
}
