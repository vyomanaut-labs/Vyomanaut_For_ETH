package org.vyomanaut.provider.power

/** Pure rule for which background-running steps must be done before the provider may start. */
object StartReadiness {
    const val NEED_APP_EXEMPTION = "Allow this app to run in the background (battery optimisation)."
    const val NEED_ZEROTIER_EXEMPTION = "Exempt the ZeroTier app from battery optimisation too."

    /** Returns the missing required steps, in the order the user should do them. Empty means ready. */
    fun missingSteps(appBatteryExempt: Boolean, zeroTierExemptConfirmed: Boolean): List<String> = buildList {
        if (!appBatteryExempt) add(NEED_APP_EXEMPTION)
        if (!zeroTierExemptConfirmed) add(NEED_ZEROTIER_EXEMPTION)
    }
}
