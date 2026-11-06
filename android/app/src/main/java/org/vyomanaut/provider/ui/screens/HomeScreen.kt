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
import org.vyomanaut.provider.config.DemoProfile
import org.vyomanaut.provider.engine.ConfigResult
import org.vyomanaut.provider.engine.EngineConfigs
import org.vyomanaut.provider.engine.EngineState
import org.vyomanaut.provider.engine.EngineStore
import org.vyomanaut.provider.engine.ProviderArgs
import org.vyomanaut.provider.engine.ProviderEngine
import org.vyomanaut.provider.network.OnboardingStore
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
    val registered = remember(engineState) {
        EngineStore(context).isRegisteredFor(OnboardingStore(context).coordinatorUrl())
    }

    var notificationsGranted by remember { mutableStateOf(hasNotificationPermission(context)) }
    var batteryExempt by remember { mutableStateOf(isIgnoringBatteryOptimizations(context)) }

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

        PixelPanel(title = if (running) "Status: online" else "Status: offline") {
            (engineState as? EngineState.Failed)?.let {
                Text(it.reason, style = MaterialTheme.typography.bodyMedium, color = PixelColors.Bad)
            }
            if (running) {
                PixelButton(text = "Stop provider", onClick = { ProviderController.stop(context) }, color = PixelColors.PanelLight)
            } else {
                PixelButton(
                    text = "Start provider",
                    enabled = binaryAvailable && registered,
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

        PixelPanel(title = "Power-ups") {
            Text(
                if (notificationsGranted) "Notifications: on" else "Notifications: off",
                color = if (notificationsGranted) PixelColors.Good else PixelColors.Bad,
            )
            Text(
                if (batteryExempt) "Battery optimisation: exempt"
                else "Battery optimisation: on. Android may stop the provider in the background.",
                color = if (batteryExempt) PixelColors.Good else PixelColors.Bad,
            )
            if (!batteryExempt) {
                PixelButton(
                    text = "Allow background running",
                    color = PixelColors.PanelLight,
                    onClick = {
                        batteryLauncher.launch(
                            Intent(
                                Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
                                Uri.parse("package:${context.packageName}"),
                            ),
                        )
                    },
                )
            }
        }

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

            registered -> Text("This phone is registered with the coordinator.", color = PixelColors.Good)

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

private fun hasNotificationPermission(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
        PackageManager.PERMISSION_GRANTED

private fun isIgnoringBatteryOptimizations(context: Context): Boolean =
    context.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(context.packageName)
