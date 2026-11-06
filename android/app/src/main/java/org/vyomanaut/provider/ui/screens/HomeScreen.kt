package org.vyomanaut.provider.ui.screens

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LifecycleResumeEffect
import org.vyomanaut.provider.config.DemoProfile
import org.vyomanaut.provider.engine.ConfigResult
import org.vyomanaut.provider.engine.EngineConfigs
import org.vyomanaut.provider.engine.EngineState
import org.vyomanaut.provider.engine.EngineStore
import org.vyomanaut.provider.engine.ProviderArgs
import org.vyomanaut.provider.engine.ProviderEngine
import org.vyomanaut.provider.engine.RestartPolicy
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.power.ChecklistStore
import org.vyomanaut.provider.power.OemGuides
import org.vyomanaut.provider.power.StartReadiness
import org.vyomanaut.provider.service.ProviderController
import org.vyomanaut.provider.service.ProviderRuntime
import org.vyomanaut.provider.storage.LimitStore
import org.vyomanaut.provider.storage.formatGiB
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel
import org.vyomanaut.provider.ui.pixel.PixelTextField
import org.vyomanaut.provider.ui.pixel.ScreenTitle

private const val LOG_LINES_SHOWN = 40
private const val LOG_FONT_SP = 11
private const val OTP_DIGITS = 6

@Composable
fun HomeScreen() {
    val context = LocalContext.current
    val running by ProviderRuntime.running.collectAsState()
    val engineState by ProviderEngine.state.collectAsState()
    val logLines by ProviderEngine.log.collectAsState()

    val binaryAvailable = remember { ProviderEngine.isBinaryAvailable(context) }
    // Read on every recomposition (a cheap preference lookup): a reset can clear it without changing the state.
    val registered = EngineStore(context).isRegisteredFor(OnboardingStore(context).coordinatorUrl())

    var notificationsGranted by remember { mutableStateOf(hasNotificationPermission(context)) }
    var batteryExempt by remember { mutableStateOf(isIgnoringBatteryOptimizations(context)) }
    val checklist = remember { ChecklistStore(context) }
    var zeroTierExempt by remember { mutableStateOf(checklist.zeroTierExempt()) }
    var oemDone by remember { mutableStateOf(checklist.oemSettingsDone()) }
    val oemGuide = remember { OemGuides.forManufacturer(Build.MANUFACTURER, Build.BRAND) }
    val missingSteps = StartReadiness.missingSteps(batteryExempt, zeroTierExempt)

    // The user may change battery or notification settings outside the app; re-read them on return.
    LifecycleResumeEffect(Unit) {
        batteryExempt = isIgnoringBatteryOptimizations(context)
        notificationsGranted = hasNotificationPermission(context)
        onPauseOrDispose { }
    }

    val notificationLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        notificationsGranted = granted
        // The service runs without the permission too; the notification is just hidden.
        ProviderController.start(context)
    }
    val batteryLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { batteryExempt = isIgnoringBatteryOptimizations(context) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ScreenTitle("Vyomanaut", "Provider node")

        // The title follows what the provider is actually doing, not whether the service was started.
        PixelPanel(title = "Status: " + statusLabel(engineState)) {
            (engineState as? EngineState.Failed)?.let {
                Text(it.reason, style = MaterialTheme.typography.bodyMedium, color = PixelColors.Bad)
            }
            (engineState as? EngineState.NeedsRegistration)?.let {
                Text(it.reason, style = MaterialTheme.typography.bodyMedium, color = PixelColors.Bad)
            }
            (engineState as? EngineState.Restarting)?.let {
                Text(
                    "${it.reason}. Restarting automatically (attempt ${it.attempt} of ${RestartPolicy.MAX_ATTEMPTS}) " +
                        "in ${it.delaySeconds} s.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = PixelColors.Gold,
                )
            }
            if (running) {
                PixelButton(text = "Stop provider", onClick = { ProviderController.stop(context) }, color = PixelColors.PanelLight)
            } else {
                PixelButton(
                    text = "Start provider",
                    enabled = binaryAvailable && registered && missingSteps.isEmpty(),
                    onClick = {
                        if (!notificationsGranted && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                            notificationLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                        } else {
                            ProviderController.start(context)
                        }
                    },
                )
                if (!binaryAvailable || !registered) {
                    Text("Register this phone below before starting.", color = PixelColors.Muted)
                }
                missingSteps.forEach { Text(it, color = PixelColors.Gold) }
            }
        }

        RegistrationPanel(binaryAvailable = binaryAvailable, registered = registered, engineState = engineState)

        if (logLines.isNotEmpty()) {
            PixelPanel(title = "Provider output") {
                // Selectable so the text can be long-pressed, copied and pasted into a bug report.
                SelectionContainer {
                    Text(
                        logLines.takeLast(LOG_LINES_SHOWN).joinToString("\n"),
                        // Monospace on purpose: Bebas Neue is all capitals, which hides case in IDs and errors.
                        fontFamily = FontFamily.Monospace,
                        fontSize = LOG_FONT_SP.sp,
                        color = PixelColors.Text,
                    )
                }
            }
        }

        ReadinessPanel(
            notificationsGranted = notificationsGranted,
            appBatteryExempt = batteryExempt,
            onRequestAppExemption = {
                batteryLauncher.launch(
                    Intent(
                        Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
                        Uri.parse("package:${context.packageName}"),
                    ),
                )
            },
            zeroTierExempt = zeroTierExempt,
            onZeroTierExemptChange = {
                zeroTierExempt = it
                checklist.setZeroTierExempt(it)
            },
            oemGuide = oemGuide,
            oemDone = oemDone,
            onOemDoneChange = {
                oemDone = it
                checklist.setOemSettingsDone(it)
            },
        )

        PixelPanel(title = "Network profile: demo") {
            Text("Erasure code RS(${DemoProfile.DATA_SHARDS},${DemoProfile.TOTAL_SHARDS}), minimum ${DemoProfile.MIN_ACTIVE_PROVIDERS} providers")
            Text("Audit period ${DemoProfile.AUDIT_PERIOD_MINUTES} min, departure after ${DemoProfile.DEPARTURE_THRESHOLD_MINUTES} min offline")
            Text("Payments: mock (prototype)")
        }
    }
}

@Composable
private fun RegistrationPanel(
    binaryAvailable: Boolean,
    registered: Boolean,
    engineState: EngineState,
) {
    val context = LocalContext.current
    var otp by remember { mutableStateOf("") }
    var message by remember { mutableStateOf<String?>(null) }
    var confirmReset by remember { mutableStateOf(false) }

    PixelPanel(title = "Registration") {
        val limitBytes = LimitStore(context).limitBytes()
        if (ProviderArgs.storageGbFromLimit(limitBytes) < DemoProfile.COORDINATOR_MIN_DECLARED_GB) {
            Text(
                "Prototype note: the coordinator requires at least ${DemoProfile.COORDINATOR_MIN_DECLARED_GB} GB, " +
                    "so this phone is registered as ${DemoProfile.COORDINATOR_MIN_DECLARED_GB} GB although you offer " +
                    "${formatGiB(limitBytes)}. Keep test files small so the phone does not run out of the space you offered.",
                color = PixelColors.Gold,
            )
        }
        when {
            !binaryAvailable -> Text(
                "The provider program is not bundled yet. Build it on your PC with " +
                    "scripts/build_android_provider.ps1, then rebuild and run the app.",
            )

            engineState is EngineState.Registering -> Text("Contacting the coordinator...")

            engineState is EngineState.AwaitingOtp -> {
                Text("The coordinator sent a 6-digit code. Ask the network operator to read it from the coordinator's console.")
                PixelTextField(
                    value = otp,
                    onValueChange = { otp = it.filter(Char::isDigit).take(OTP_DIGITS) },
                    label = "6-digit code",
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                )
                PixelButton(text = "Submit code", onClick = {
                    message = if (ProviderEngine.submitOtp(otp)) null else "The code must be exactly 6 digits."
                })
            }

            engineState is EngineState.Verifying -> Text("Verifying the code...")

            engineState is EngineState.NeedsRegistration -> {
                Text(
                    "Registering again deletes this phone's provider identity and stored shards, then runs " +
                        "onboarding with a new 6-digit code.",
                )
                PixelButton(text = "Register again", onClick = { ProviderEngine.resetRegistration(context) })
            }

            registered -> {
                Text("This phone is registered with the coordinator.", color = PixelColors.Good)
                // Two taps, because this wipes the provider identity and every stored shard.
                PixelButton(
                    text = if (confirmReset) "Tap again to delete and reset" else "Reset registration",
                    color = if (confirmReset) PixelColors.Bad else PixelColors.PanelLight,
                    onClick = {
                        if (confirmReset) {
                            confirmReset = false
                            ProviderController.stop(context)
                            ProviderEngine.resetRegistration(context)
                        } else {
                            confirmReset = true
                        }
                    },
                )
            }

            else -> {
                Text("Register once so the coordinator admits this phone. Make sure the Network and Storage tabs are set up first.")
                PixelButton(text = "Register this phone", onClick = {
                    message = null
                    when (val result = EngineConfigs.fromDevice(context, requireRegistered = false)) {
                        is ConfigResult.Missing -> message = result.reason
                        is ConfigResult.Ready -> ProviderEngine.startOnboarding(context, result.config)
                    }
                })
            }
        }
        message?.let { Text(it, color = PixelColors.Bad) }
    }
}

private fun statusLabel(state: EngineState): String = when (state) {
    EngineState.Running -> "online"
    EngineState.Starting -> "starting"
    is EngineState.Restarting -> "reconnecting"
    is EngineState.NeedsRegistration -> "register again"
    EngineState.Registering, EngineState.AwaitingOtp, EngineState.Verifying -> "registering"
    else -> "offline"
}

private fun hasNotificationPermission(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
        PackageManager.PERMISSION_GRANTED

private fun isIgnoringBatteryOptimizations(context: Context): Boolean =
    context.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(context.packageName)
