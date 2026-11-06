package org.vyomanaut.provider.service

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import org.vyomanaut.provider.MainActivity
import org.vyomanaut.provider.R
import org.vyomanaut.provider.engine.ConfigResult
import org.vyomanaut.provider.engine.EngineConfigs
import org.vyomanaut.provider.engine.EngineState
import org.vyomanaut.provider.engine.ProviderEngine

/**
 * Foreground service that keeps the provider alive while the phone is in the background.
 * It starts the Go provider program on start and stops it when the service ends.
 */
class ProviderService : Service() {

    private val locks by lazy { PowerLocks(this) }
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }
        ensureChannel()
        ServiceCompat.startForeground(
            this,
            NOTIFICATION_ID,
            buildNotification(),
            ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
        )
        locks.acquire()
        ProviderRuntime.setRunning(true)

        when (val result = EngineConfigs.fromDevice(this, requireRegistered = true)) {
            is ConfigResult.Missing -> {
                ProviderEngine.fail(result.reason)
                stopSelf()
                return START_NOT_STICKY
            }
            is ConfigResult.Ready -> ProviderEngine.startRun(this, result.config)
        }
        // Do not keep a "running" service around a provider that has failed.
        scope.launch {
            ProviderEngine.state.first { it is EngineState.Failed }
            stopSelf()
        }
        return START_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        ProviderEngine.shutdown()
        locks.release()
        ProviderRuntime.setRunning(false)
        super.onDestroy()
    }

    private fun ensureChannel() {
        val manager = getSystemService(NotificationManager::class.java)
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.app_name),
            NotificationManager.IMPORTANCE_LOW,
        )
        manager.createNotificationChannel(channel)
    }

    private fun buildNotification(): android.app.Notification {
        val openApp = PendingIntent.getActivity(
            this,
            REQUEST_OPEN,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this,
            REQUEST_STOP,
            Intent(this, ProviderService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle(getString(R.string.app_name))
            .setContentText("Provider is running")
            .setContentIntent(openApp)
            .addAction(0, "Stop", stop)
            .setOngoing(true)
            .build()
    }

    companion object {
        const val ACTION_START = "org.vyomanaut.provider.action.START"
        const val ACTION_STOP = "org.vyomanaut.provider.action.STOP"
        private const val CHANNEL_ID = "provider_service"
        private const val NOTIFICATION_ID = 1
        private const val REQUEST_OPEN = 10
        private const val REQUEST_STOP = 11
    }
}
