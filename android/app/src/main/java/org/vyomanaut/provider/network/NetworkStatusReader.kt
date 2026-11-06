package org.vyomanaut.provider.network

import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import java.net.Inet4Address

data class NetworkStatus(
    val zeroTierInstalled: Boolean,
    val vpnActive: Boolean,
    /** IPv4 addresses on VPN networks. With ZeroTier running this includes the phone's ZeroTier address. */
    val vpnAddresses: List<String>,
)

object NetworkStatusReader {
    const val ZEROTIER_PACKAGE = "com.zerotier.one"

    // allNetworks is deprecated, but ZeroTier's VPN is not the default route, so activeNetwork would miss it.
    @Suppress("DEPRECATION")
    fun read(context: Context): NetworkStatus {
        val manager = context.getSystemService(ConnectivityManager::class.java)
        val vpnNetworks = manager.allNetworks.filter { network ->
            manager.getNetworkCapabilities(network)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
        }
        val addresses = vpnNetworks
            .mapNotNull { manager.getLinkProperties(it) }
            .flatMap { it.linkAddresses }
            .map { it.address }
            .filterIsInstance<Inet4Address>()
            .mapNotNull { it.hostAddress }
        return NetworkStatus(
            zeroTierInstalled = isInstalled(context, ZEROTIER_PACKAGE),
            vpnActive = vpnNetworks.isNotEmpty(),
            vpnAddresses = addresses,
        )
    }

    @Suppress("DEPRECATION")
    private fun isInstalled(context: Context, packageName: String): Boolean = try {
        context.packageManager.getPackageInfo(packageName, 0)
        true
    } catch (e: PackageManager.NameNotFoundException) {
        false
    }
}
