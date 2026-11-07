package org.vyomanaut.provider.ui.screens

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings

/** Opens another app's settings page (battery, permissions). */
internal fun openAppSettings(context: Context, packageName: String) {
    launch(context, Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
}

internal fun openWebPage(context: Context, url: String) {
    launch(context, Intent(Intent.ACTION_VIEW, Uri.parse(url)))
}

private fun launch(context: Context, intent: Intent) {
    try {
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (e: ActivityNotFoundException) {
        // Some phones lack the screen; nothing else to open.
    }
}
