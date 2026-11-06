package org.vyomanaut.provider.ui.screens

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import org.vyomanaut.provider.network.NetworkStatusReader
import org.vyomanaut.provider.power.OemGuide
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel

/**
 * "Before you start" checklist. The two battery steps are required (Start stays disabled without them);
 * the phone-maker step is shown only on brands that kill background apps beyond stock Android.
 */
@Composable
fun ReadinessPanel(
    notificationsGranted: Boolean,
    appBatteryExempt: Boolean,
    onRequestAppExemption: () -> Unit,
    zeroTierExempt: Boolean,
    onZeroTierExemptChange: (Boolean) -> Unit,
    oemGuide: OemGuide?,
    oemDone: Boolean,
    onOemDoneChange: (Boolean) -> Unit,
) {
    val context = LocalContext.current
    PixelPanel(title = "Before you start") {
        Text(
            "Android stops background apps on battery. Do these once, or the provider goes offline when the screen turns off.",
            color = PixelColors.Muted,
        )

        StepLine(done = appBatteryExempt, label = "1. This app: battery optimisation off (required)")
        if (!appBatteryExempt) {
            PixelButton(text = "Allow background running", color = PixelColors.PanelLight, onClick = onRequestAppExemption)
        }

        StepLine(done = zeroTierExempt, label = "2. ZeroTier app: battery optimisation off (required)")
        Text(
            "Open ZeroTier's settings, then Battery > Unrestricted (or Don't optimise). " +
                "If ZeroTier sleeps, the coordinator cannot reach this phone.",
            style = MaterialTheme.typography.bodySmall,
        )
        PixelButton(
            text = "Open ZeroTier settings",
            color = PixelColors.PanelLight,
            onClick = { openAppSettings(context, NetworkStatusReader.ZEROTIER_PACKAGE) },
        )
        PixelButton(
            text = if (zeroTierExempt) "Undo: ZeroTier not exempt" else "I exempted ZeroTier",
            color = if (zeroTierExempt) PixelColors.PanelDark else PixelColors.Accent,
            onClick = { onZeroTierExemptChange(!zeroTierExempt) },
        )

        if (oemGuide != null) {
            StepLine(done = oemDone, label = "3. ${oemGuide.brand} extra settings (recommended)")
            Text(oemGuide.steps, style = MaterialTheme.typography.bodySmall)
            PixelButton(text = "Open step-by-step guide", color = PixelColors.PanelLight, onClick = { openUrl(context, oemGuide.url) })
            PixelButton(text = "Open this app's settings", color = PixelColors.PanelLight, onClick = { openAppSettings(context, context.packageName) })
            PixelButton(
                text = if (oemDone) "Undo: not done" else "I changed these settings",
                color = if (oemDone) PixelColors.PanelDark else PixelColors.Accent,
                onClick = { onOemDoneChange(!oemDone) },
            )
        }

        StepLine(done = notificationsGranted, label = "Notifications (optional): shows that the provider is running")
    }
}

@Composable
private fun StepLine(done: Boolean, label: String) {
    Text(
        (if (done) "[done] " else "[todo] ") + label,
        color = if (done) PixelColors.Good else PixelColors.Bad,
    )
}

private fun openAppSettings(context: Context, packageName: String) {
    launch(context, Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
}

private fun openUrl(context: Context, url: String) {
    launch(context, Intent(Intent.ACTION_VIEW, Uri.parse(url)))
}

private fun launch(context: Context, intent: Intent) {
    try {
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (e: ActivityNotFoundException) {
        // Some phones lack the screen; nothing else to open.
    }
}
