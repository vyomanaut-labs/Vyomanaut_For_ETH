package org.vyomanaut.provider.engine

import android.content.Context
import org.vyomanaut.provider.network.NetworkStatusReader
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.storage.LimitStore
import java.io.File

/** Gathers the saved settings and the phone's current ZeroTier address into an engine configuration. */
object EngineConfigs {
    private const val DATA_SUBDIR = "provider"

    fun fromDevice(context: Context, requireRegistered: Boolean): ConfigResult {
        val onboarding = OnboardingStore(context)
        val url = onboarding.coordinatorUrl()
        if (requireRegistered && !EngineStore(context).isRegisteredFor(url)) {
            return ConfigResult.Missing("Register this phone first (Home tab).")
        }
        return EngineConfigBuilder.build(
            coordinatorUrl = url,
            phone = onboarding.phone(),
            limitBytes = LimitStore(context).limitBytes(),
            vpnAddresses = NetworkStatusReader.read(context).vpnAddresses,
            dataDir = File(context.filesDir, DATA_SUBDIR).absolutePath,
        )
    }
}
