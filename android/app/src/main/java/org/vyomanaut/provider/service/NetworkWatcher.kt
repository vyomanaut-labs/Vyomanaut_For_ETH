package org.vyomanaut.provider.service

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Handler
import android.os.Looper

/**
 * Tells the engine when the phone's networks change, so the provider recovers without the user pressing
 * Stop and Start (review: "Other reconnect weak points"). Watches VPN networks (ZeroTier) and the default
 * network (Wi-Fi or mobile data). Bursts of callbacks are collapsed into one report after [SETTLE_MS].
 *
 * [onChange] gets `vpnReturned = true` when a VPN network came up after all VPN networks had been lost.
 */
class NetworkWatcher(context: Context, private val onChange: (vpnReturned: Boolean) -> Unit) {
    private val manager = context.applicationContext.getSystemService(ConnectivityManager::class.java)
    private val handler = Handler(Looper.getMainLooper())
    private val vpnNetworks = mutableSetOf<Network>()
    private var vpnLost = false
    private var vpnReturned = false
    private var registered = false

    private val report = Runnable {
        val returned = vpnReturned
        vpnReturned = false
        onChange(returned)
    }

    private val vpnCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = handler.post {
            if (vpnNetworks.isEmpty() && vpnLost) {
                vpnReturned = true
                vpnLost = false
            }
            vpnNetworks += network
            schedule()
        }.let { }

        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) =
            handler.post { schedule() }.let { }

        override fun onLost(network: Network) = handler.post {
            vpnNetworks -= network
            if (vpnNetworks.isEmpty()) vpnLost = true
            schedule()
        }.let { }
    }

    private val defaultCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = handler.post { schedule() }.let { }
    }

    fun start() {
        if (registered) return
        val vpnRequest = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_VPN)
            .removeCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        manager.registerNetworkCallback(vpnRequest, vpnCallback)
        manager.registerDefaultNetworkCallback(defaultCallback)
        registered = true
    }

    fun stop() {
        if (!registered) return
        manager.unregisterNetworkCallback(vpnCallback)
        manager.unregisterNetworkCallback(defaultCallback)
        handler.removeCallbacks(report)
        registered = false
    }

    private fun schedule() {
        handler.removeCallbacks(report)
        handler.postDelayed(report, SETTLE_MS)
    }

    private companion object {
        const val SETTLE_MS = 3_000L
    }
}
