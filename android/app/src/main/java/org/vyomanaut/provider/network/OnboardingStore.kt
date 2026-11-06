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

    /** The relay the user typed, or "" to use the default (see [relayAddr]). */
    fun savedRelay(): String = prefs.getString(KEY_RELAY, "") ?: ""

    fun saveRelay(relay: String) {
        prefs.edit().putString(KEY_RELAY, relay.trim()).apply()
    }

    /** The relay to use: the saved one, or the coordinator's host on the default relay port. "off" disables it. */
    fun relayAddr(): String? = RelayRules.effectiveRelay(savedRelay(), coordinatorUrl())

    /** The joined network's name, from its join link; "" when entered by hand. */
    fun networkName(): String = prefs.getString(KEY_NETWORK_NAME, "") ?: ""

    fun saveNetworkName(name: String) {
        prefs.edit().putString(KEY_NETWORK_NAME, name).apply()
    }

    private companion object {
        const val KEY_NETWORK_NAME = "network_name"
        const val PREFS_NAME = "provider_settings"
        const val KEY_COORDINATOR_URL = "coordinator_url"
        const val KEY_PHONE = "phone"
        const val KEY_RELAY = "relay_addr"
    }
}
