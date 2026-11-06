package org.vyomanaut.provider.engine

import android.content.Context

/** Remembers which coordinator this phone has registered with. */
class EngineStore(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun isRegisteredFor(coordinatorUrl: String): Boolean =
        coordinatorUrl.isNotEmpty() && prefs.getString(KEY_REGISTERED_COORDINATOR, "") == coordinatorUrl

    fun markRegistered(coordinatorUrl: String) {
        prefs.edit().putString(KEY_REGISTERED_COORDINATOR, coordinatorUrl).apply()
    }

    /** Forgets the registration, so Start is blocked until the phone registers again. */
    fun clearRegistration() {
        prefs.edit().remove(KEY_REGISTERED_COORDINATOR).apply()
    }

    private companion object {
        const val PREFS_NAME = "provider_settings"
        const val KEY_REGISTERED_COORDINATOR = "registered_coordinator"
    }
}
