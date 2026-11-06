package org.vyomanaut.provider.network

import java.net.URI
import java.net.URISyntaxException

/**
 * Pure rules for the relay setting. The relay (`go run ./cmd/relay`) normally runs next to the coordinator, so
 * the default is the coordinator's host on [DEFAULT_RELAY_PORT].
 */
object RelayRules {
    const val DEFAULT_RELAY_PORT = 30300
    const val OFF = "off"
    private val hostPort = Regex("^[A-Za-z0-9.-]+:[0-9]{1,5}$")

    /** The default relay for a coordinator URL, or null when the URL has no host. */
    fun defaultFor(coordinatorUrl: String): String? = try {
        URI(coordinatorUrl.trim()).host?.takeIf { it.isNotEmpty() }?.let { "$it:$DEFAULT_RELAY_PORT" }
    } catch (e: URISyntaxException) {
        null
    }

    /** "" means default, "off" means none, anything else must be host:port. */
    fun effectiveRelay(saved: String, coordinatorUrl: String): String? = when {
        saved.equals(OFF, ignoreCase = true) -> null
        saved.isBlank() -> defaultFor(coordinatorUrl)
        else -> saved.trim()
    }

    /** Null when [input] is usable (blank, "off" or host:port), otherwise the problem to show. */
    fun problemWith(input: String): String? {
        val t = input.trim()
        if (t.isEmpty() || t.equals(OFF, ignoreCase = true)) return null
        if (!hostPort.matches(t)) return "Enter the relay as host:port, for example 10.35.114.52:30300, or off."
        val port = t.substringAfterLast(':').toInt()
        return if (port in 1..65535) null else "The relay port must be between 1 and 65535."
    }
}
