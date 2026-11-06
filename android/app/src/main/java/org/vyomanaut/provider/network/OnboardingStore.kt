package org.vyomanaut.provider.network

import android.content.Context

/** Remembers the coordinator address and phone number the provider entered. */
class OnboardingStore(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun coordinatorUrl(): String = prefs.getString(KEY_COORDINATOR_URL, "") ?: ""

    fun phone(): String = prefs.getString(KEY_PHONE, "") ?: ""

    fun save(coordinatorUrl: String, phone: String) {
        prefs.edit().putString(KEY_COORDINATOR_URL, coordinatorUrl).putString(KEY_PHONE, phone).apply()
    }

    private companion object {
        const val PREFS_NAME = "provider_settings"
        const val KEY_COORDINATOR_URL = "coordinator_url"
        const val KEY_PHONE = "phone"
    }
}
