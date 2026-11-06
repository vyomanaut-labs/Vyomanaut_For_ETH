package org.vyomanaut.provider.network

import java.net.URI
import java.net.URISyntaxException
import kotlin.math.abs

/** Pure input rules for the onboarding screen. No Android types, so they are unit-tested on the host. */
object NetworkRules {
    private const val MIN_PHONE_DIGITS = 10
    private const val MAX_PHONE_DIGITS = 15

    /** A phone clock further than this from the coordinator may break signed requests. Advisory only. */
    const val CLOCK_SKEW_WARN_SECONDS = 30L

    private val phoneFormat = Regex("^\\+?[0-9]{$MIN_PHONE_DIGITS,$MAX_PHONE_DIGITS}$")
    private val phoneSeparators = Regex("[\\s\\-()]")
    private val allowedSchemes = setOf("http", "https")

    /** Returns the base URL without a trailing slash, or null when it is not a usable http(s) address. */
    fun normalizeBaseUrl(input: String): String? {
        val trimmed = input.trim().trimEnd('/')
        if (!trimmed.contains("://")) return null
        return try {
            val uri = URI(trimmed)
            val scheme = uri.scheme?.lowercase()
            if (scheme !in allowedSchemes || uri.host.isNullOrEmpty()) null else trimmed
        } catch (e: URISyntaxException) {
            null
        }
    }

    /** Returns the phone number with separators removed, or null when it does not look like a phone number. */
    fun normalizePhone(input: String): String? {
        val cleaned = input.trim().replace(phoneSeparators, "")
        return if (phoneFormat.matches(cleaned)) cleaned else null
    }

    /** Positive when the phone clock is ahead of the coordinator. */
    fun clockSkewSeconds(phoneEpochSeconds: Long, serverEpochSeconds: Long): Long =
        phoneEpochSeconds - serverEpochSeconds

    fun isClockSkewTooLarge(skewSeconds: Long): Boolean = abs(skewSeconds) > CLOCK_SKEW_WARN_SECONDS
}
