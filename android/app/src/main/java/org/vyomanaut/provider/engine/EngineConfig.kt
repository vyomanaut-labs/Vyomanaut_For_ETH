package org.vyomanaut.provider.engine

import org.vyomanaut.provider.config.DemoProfile
import org.vyomanaut.provider.storage.BYTES_PER_GIB

/** Everything the Go provider needs on its command line. */
data class EngineConfig(
    val coordinatorUrl: String,
    val phone: String,
    /** The size declared to the coordinator. May exceed the limit the provider chose (see DemoProfile). */
    val storageGb: Int,
    val listenPort: Int,
    /** The phone's address on the coordinator's network (Wi-Fi or ZeroTier), advertised so others can reach it. */
    val advertiseAddr: String,
    val dataDir: String,
    /**
     * Relay node ("host:port") the provider stays reachable through, or null. Needed on ZeroTier: Android never
     * delivers new incoming VPN connections to an app, so audits and uploads must come in through a relay.
     */
    val relayAddr: String? = null,
)

sealed interface ConfigResult {
    data class Ready(val config: EngineConfig) : ConfigResult
    /** [retryable] is true when the setting can fix itself (network reconnecting), so a restart may wait for it. */
    data class Missing(val reason: String, val retryable: Boolean = false) : ConfigResult
}

/** Pure rules for turning the app's saved settings into a provider configuration. */
object EngineConfigBuilder {

    fun build(
        coordinatorUrl: String,
        phone: String,
        limitBytes: Long,
        vpnAddresses: List<String>,
        dataDir: String,
        listenPort: Int = DemoProfile.DEFAULT_LISTEN_PORT,
    ): ConfigResult {
        if (coordinatorUrl.isBlank() || phone.isBlank()) {
            return ConfigResult.Missing("Enter and test the coordinator address and phone number on the Network tab.")
        }
        val limitGb = ProviderArgs.storageGbFromLimit(limitBytes)
        if (limitGb < 1) {
            return ConfigResult.Missing("Save a storage limit of at least 1 GB on the Storage tab.")
        }
        // Prototype workaround: the coordinator rejects less than COORDINATOR_MIN_DECLARED_GB.
        val storageGb = maxOf(limitGb, DemoProfile.COORDINATOR_MIN_DECLARED_GB)
        val address = vpnAddresses.firstOrNull()
            ?: return ConfigResult.Missing(
                "Connect this phone to the coordinator's network (the same Wi-Fi, or ZeroTier).",
                retryable = true,
            )
        return ConfigResult.Ready(
            EngineConfig(
                coordinatorUrl = coordinatorUrl,
                phone = phone,
                storageGb = storageGb,
                listenPort = listenPort,
                advertiseAddr = address,
                dataDir = dataDir,
            ),
        )
    }
}

/** Builds the argument lists, mirroring how the Windows installer calls the same provider program. */
object ProviderArgs {
    private const val OTP_LENGTH = 6
    private val otpFormat = Regex("^[0-9]{$OTP_LENGTH}$")

    /** Whole GiB; never more than the limit the provider chose, whichever way the provider defines a GB. */
    fun storageGbFromLimit(limitBytes: Long): Int =
        (limitBytes / BYTES_PER_GIB).coerceIn(0L, Int.MAX_VALUE.toLong()).toInt()

    fun isValidOtp(code: String): Boolean = otpFormat.matches(code)

    fun onboard(config: EngineConfig): List<String> = listOf(
        "onboard",
        "--microservice-url=${config.coordinatorUrl}",
        "--phone=${config.phone}",
        "--storage-gb=${config.storageGb}",
        "--data-dir=${config.dataDir}",
        "--listen-port=${config.listenPort}",
        "--advertise-addr=${config.advertiseAddr}",
    )

    fun run(config: EngineConfig): List<String> = listOfNotNull(
        "run",
        "--mode=demo",
        "--microservice-url=${config.coordinatorUrl}",
        config.relayAddr?.takeIf { it.isNotBlank() }?.let { "--relay-addrs=$it" },
        "--data-dir=${config.dataDir}",
        "--declared-storage-gb=${config.storageGb}",
        "--listen-port=${config.listenPort}",
        "--advertise-addr=${config.advertiseAddr}",
    )
}
