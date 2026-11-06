package org.vyomanaut.provider.engine

import android.content.Context
import org.vyomanaut.provider.network.AddressPicker
import org.vyomanaut.provider.network.NetworkStatusReader
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.storage.LimitStore
import java.io.File
import java.net.URI

/** Gathers the saved settings and the phone's current ZeroTier address into an engine configuration. */
object EngineConfigs {
    private const val DATA_SUBDIR = "provider"

    /** The provider's data directory: identity, registration.json and stored shards. */
    fun dataDir(context: Context): File = File(context.filesDir, DATA_SUBDIR)

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
            vpnAddresses = listOfNotNull(zeroTierAddress(context, url)),
            dataDir = dataDir(context).absolutePath,
        )
    }

    /** The VPN address to advertise: the one on the coordinator's ZeroTier subnet (see [AddressPicker]). */
    fun zeroTierAddress(context: Context, coordinatorUrl: String): String? {
        val host = try {
            URI(coordinatorUrl).host
        } catch (e: java.net.URISyntaxException) {
            null
        }
        return AddressPicker.pick(NetworkStatusReader.read(context).vpnLinkAddresses, host)
    }
}
