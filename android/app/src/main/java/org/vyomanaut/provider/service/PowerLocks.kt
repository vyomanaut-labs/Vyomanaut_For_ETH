package org.vyomanaut.provider.service

import android.annotation.SuppressLint
import android.content.Context
import android.net.wifi.WifiManager
import android.os.Build
import android.os.PowerManager

/**
 * Keeps the CPU and Wi-Fi awake while the provider runs.
 *
 * A foreground service only stops Android from killing the process; it does not stop the phone from
 * suspending. Without a partial wake lock the Go provider freezes as soon as the screen goes off on
 * battery, misses its 2-minute audits and heartbeats, and the coordinator marks the phone departed.
 * The Wi-Fi locks keep the radio out of power-save so inbound audit requests on port 30303 arrive.
 */
class PowerLocks(context: Context) {
    private val appContext = context.applicationContext
    private var wakeLock: PowerManager.WakeLock? = null
    private val wifiLocks = mutableListOf<WifiManager.WifiLock>()

    // No timeout on purpose: the lock lives exactly as long as the foreground service and is released in onDestroy.
    @SuppressLint("WakelockTimeout")
    @Synchronized
    fun acquire() {
        if (wakeLock?.isHeld == true) return
        val power = appContext.getSystemService(PowerManager::class.java)
        wakeLock = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, WAKE_TAG).apply {
            setReferenceCounted(false)
            acquire()
        }
        val wifi = appContext.getSystemService(WifiManager::class.java) ?: return
        for (mode in wifiModes()) {
            wifiLocks += wifi.createWifiLock(mode, WIFI_TAG).apply {
                setReferenceCounted(false)
                acquire()
            }
        }
    }

    @Synchronized
    fun release() {
        wakeLock?.takeIf { it.isHeld }?.release()
        wakeLock = null
        wifiLocks.filter { it.isHeld }.forEach { it.release() }
        wifiLocks.clear()
    }

    /**
     * Low-latency mode (API 29+) is what the review asked for, but Android only honours it while the app is
     * visible. High-performance mode covers the screen-off case on the Android versions that still honour it,
     * so both are held.
     */
    @Suppress("DEPRECATION")
    private fun wifiModes(): List<Int> = buildList {
        add(WifiManager.WIFI_MODE_FULL_HIGH_PERF)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) add(WifiManager.WIFI_MODE_FULL_LOW_LATENCY)
    }

    private companion object {
        const val WAKE_TAG = "Vyomanaut:provider"
        const val WIFI_TAG = "Vyomanaut:provider-wifi"
    }
}
