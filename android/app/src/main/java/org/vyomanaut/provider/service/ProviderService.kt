package org.vyomanaut.provider.service

import android.app.Notification
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
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import org.vyomanaut.provider.MainActivity
import org.vyomanaut.provider.R
import org.vyomanaut.provider.engine.ConfigResult
import org.vyomanaut.provider.engine.EngineConfigs
import org.vyomanaut.provider.engine.EngineState
import org.vyomanaut.provider.engine.ProviderEngine
import org.vyomanaut.provider.engine.isActive

/**
 * Foreground service that keeps the provider alive while the phone is in the background.
 *
 * The provider itself is supervised by [ProviderEngine]: this service starts it once, mirrors the engine's
 * real state in the notification, and stops itself only when the engine gives up. It never claims "running"
 * over a provider that has died.
 */
class ProviderService : Service() {

    private val locks by lazy { PowerLocks(this) }
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var watchJob: Job? = null

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
            buildNotification(textFor(ProviderEngine.state.value)),
            ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
        )
        locks.acquire()
        ProviderRuntime.setRunning(true)

        // A second start (Start pressed twice, or Android redelivering a sticky start) must not restart a provider
        // that is already up or recovering.
        if (watchJob != null && ProviderEngine.state.value.isActive) return START_STICKY

        val check = EngineConfigs.fromDevice(this, requireRegistered = true)
        if (check is ConfigResult.Missing && !check.retryable) {
            ProviderEngine.fail(check.reason)
            stopSelf()
            return START_NOT_STICKY
        }
        // Rebuilt by the engine on every (re)start, so a retryable gap such as ZeroTier still connecting is fine.
        ProviderEngine.startRun(this)
        watchJob?.cancel()
        watchJob = scope.launch {
            ProviderEngine.state.collect { state ->
                getSystemService(NotificationManager::class.java)
                    .notify(NOTIFICATION_ID, buildNotification(textFor(state)))
                if (state is EngineState.Failed || state is EngineState.NeedsRegistration) stopSelf()
            }
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

    private fun textFor(state: EngineState): String = when (state) {
        EngineState.Running -> "Online: storing shards and answering audits"
        EngineState.Starting -> "Starting the provider..."
        is EngineState.Restarting -> "Reconnecting: restart ${state.attempt} in ${state.delaySeconds} s"
        is EngineState.Failed -> "Stopped: ${state.reason}"
        is EngineState.NeedsRegistration -> "Stopped: register this phone again"
        else -> "Stopping..."
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

    private fun buildNotification(text: String): Notification {
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
            .setContentText(text)
            .setContentIntent(openApp)
            .addAction(0, "Stop", stop)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
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
