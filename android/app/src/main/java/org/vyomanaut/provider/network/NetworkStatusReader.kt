package org.vyomanaut.provider.network

import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import java.net.Inet4Address
import java.net.NetworkInterface
import java.net.SocketException

data class NetworkStatus(
    val zeroTierInstalled: Boolean,
    val vpnActive: Boolean,
    /** IPv4 addresses on VPN networks, with prefix lengths. With ZeroTier on this includes its address. */
    val vpnLinkAddresses: List<NetAddress>,
    /** IPv4 addresses on every other interface: Wi-Fi, hotspot, USB tethering, mobile data. */
    val otherLinkAddresses: List<NetAddress>,
) {
    val vpnAddresses: List<String> get() = vpnLinkAddresses.map { it.address }

    /** Every candidate address, VPN first. */
    val allAddresses: List<NetAddress> get() = vpnLinkAddresses + otherLinkAddresses
}

object NetworkStatusReader {
    const val ZEROTIER_PACKAGE = "com.zerotier.one"
    private const val LINK_LOCAL_PREFIX = "169.254."

    // allNetworks is deprecated, but ZeroTier's VPN is not the default route, so activeNetwork would miss it.
    @Suppress("DEPRECATION")
    fun read(context: Context): NetworkStatus {
        val manager = context.getSystemService(ConnectivityManager::class.java)
        val vpnNetworks = manager.allNetworks.filter { network ->
            manager.getNetworkCapabilities(network)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
        }
        val vpnLinks = vpnNetworks.mapNotNull { manager.getLinkProperties(it) }
        val vpnInterfaces = vpnLinks.mapNotNull { it.interfaceName }.toSet()
        val vpnAddresses = vpnLinks
            .flatMap { it.linkAddresses }
            .filter { it.address is Inet4Address }
            .mapNotNull { link -> link.address.hostAddress?.let { NetAddress(it, link.prefixLength, isVpn = true) } }
        return NetworkStatus(
            zeroTierInstalled = isInstalled(context, ZEROTIER_PACKAGE),
            vpnActive = vpnNetworks.isNotEmpty(),
            vpnLinkAddresses = vpnAddresses,
            otherLinkAddresses = otherAddresses(vpnInterfaces, vpnAddresses.map { it.address }.toSet()),
        )
    }

    /**
     * Read from the interfaces themselves rather than ConnectivityManager, so a hotspot or USB tether the phone
     * itself provides (which is not an Android "network") is included too.
     */
    private fun otherAddresses(vpnInterfaces: Set<String>, vpnAddresses: Set<String>): List<NetAddress> = try {
        NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()
            .filter { it.isUp && !it.isLoopback && it.name !in vpnInterfaces }
            .flatMap { it.interfaceAddresses }
            .filter { it.address is Inet4Address }
            .mapNotNull { a -> a.address.hostAddress?.let { NetAddress(it, a.networkPrefixLength.toInt(), isVpn = false) } }
            .filter { !it.address.startsWith(LINK_LOCAL_PREFIX) && it.address !in vpnAddresses }
    } catch (e: SocketException) {
        emptyList()
    }

    @Suppress("DEPRECATION")
    private fun isInstalled(context: Context, packageName: String): Boolean = try {
        context.packageManager.getPackageInfo(packageName, 0)
        true
    } catch (e: PackageManager.NameNotFoundException) {
        false
    }
}
