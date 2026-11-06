package org.vyomanaut.provider.storage

import android.content.Context

/** Persists the provider's chosen storage limit. 0 means the provider has not chosen one yet. */
class LimitStore(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun limitBytes(): Long = prefs.getLong(KEY_LIMIT_BYTES, NOT_SET)

    fun save(bytes: Long) {
        prefs.edit().putLong(KEY_LIMIT_BYTES, bytes).apply()
    }

    private companion object {
        const val PREFS_NAME = "provider_settings"
        const val KEY_LIMIT_BYTES = "limit_bytes"
        const val NOT_SET = 0L
    }
}
