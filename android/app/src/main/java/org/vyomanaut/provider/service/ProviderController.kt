package org.vyomanaut.provider.service

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat

/** Starts and stops the provider foreground service. */
object ProviderController {
    fun start(context: Context) {
        val intent = Intent(context, ProviderService::class.java).setAction(ProviderService.ACTION_START)
        ContextCompat.startForegroundService(context, intent)
    }

    fun stop(context: Context) {
        context.stopService(Intent(context, ProviderService::class.java))
    }
}
