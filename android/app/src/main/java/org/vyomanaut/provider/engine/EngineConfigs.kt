package org.vyomanaut.provider.engine

import android.content.Context
import org.vyomanaut.provider.network.AddressPicker
import org.vyomanaut.provider.network.NetAddress
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
        val built = EngineConfigBuilder.build(
            coordinatorUrl = url,
            phone = onboarding.phone(),
            limitBytes = LimitStore(context).limitBytes(),
            vpnAddresses = listOfNotNull(advertiseAddress(context, url)?.address),
            dataDir = dataDir(context).absolutePath,
        )
        return if (built is ConfigResult.Ready) ConfigResult.Ready(built.config.copy(relayAddr = onboarding.relayAddr())) else built
    }

    /** The address to advertise: the one on the coordinator's network, Wi-Fi or ZeroTier (see [AddressPicker]). */
    fun advertiseAddress(context: Context, coordinatorUrl: String): NetAddress? {
        val host = try {
            URI(coordinatorUrl).host
        } catch (e: java.net.URISyntaxException) {
            null
        }
        return AddressPicker.pick(NetworkStatusReader.read(context).allAddresses, host)
    }
}
