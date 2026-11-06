package org.vyomanaut.provider.power

import android.content.Context

/**
 * Remembers the checklist steps the app cannot verify on its own: Android does not let one app read
 * another app's battery exemption, so the provider confirms ZeroTier and phone-maker settings by hand.
 */
class ChecklistStore(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun zeroTierExempt(): Boolean = prefs.getBoolean(KEY_ZEROTIER_EXEMPT, false)

    fun setZeroTierExempt(done: Boolean) {
        prefs.edit().putBoolean(KEY_ZEROTIER_EXEMPT, done).apply()
    }

    fun oemSettingsDone(): Boolean = prefs.getBoolean(KEY_OEM_DONE, false)

    fun setOemSettingsDone(done: Boolean) {
        prefs.edit().putBoolean(KEY_OEM_DONE, done).apply()
    }

    private companion object {
        const val PREFS_NAME = "provider_checklist"
        const val KEY_ZEROTIER_EXEMPT = "zerotier_battery_exempt"
        const val KEY_OEM_DONE = "oem_settings_done"
    }
}
