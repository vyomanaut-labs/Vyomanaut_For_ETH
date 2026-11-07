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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
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
import org.vyomanaut.provider.network.CheckResult
import org.vyomanaut.provider.network.CoordinatorCheck
import org.vyomanaut.provider.network.NetworkStatusReader
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.power.ChecklistStore
import org.vyomanaut.provider.power.OemGuides
import org.vyomanaut.provider.power.SetupInputs
import org.vyomanaut.provider.power.SetupSteps
import org.vyomanaut.provider.power.StepAction
import org.vyomanaut.provider.service.ProviderController
import org.vyomanaut.provider.service.ProviderRuntime
import org.vyomanaut.provider.storage.BYTES_PER_GIB
import org.vyomanaut.provider.storage.LimitRange
import org.vyomanaut.provider.storage.LimitStore
import org.vyomanaut.provider.storage.StatFsFreeSpace
import org.vyomanaut.provider.storage.StorageLimitPolicy
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
fun HomeScreen(onOpenNetwork: () -> Unit = {}, onOpenStorage: () -> Unit = {}) {
    val context = LocalContext.current
    val running by ProviderRuntime.running.collectAsState()
    val engineState by ProviderEngine.state.collectAsState()
    val logLines by ProviderEngine.log.collectAsState()

    val binaryAvailable = remember { ProviderEngine.isBinaryAvailable(context) }
    val onboarding = OnboardingStore(context)
    val coordinatorUrl = onboarding.coordinatorUrl()
    // Read on every recomposition (a cheap preference lookup): a reset can clear it without changing the state.
    val registered = EngineStore(context).isRegisteredFor(coordinatorUrl)

    var notificationsGranted by remember { mutableStateOf(hasNotificationPermission(context)) }
    var batteryExempt by remember { mutableStateOf(isIgnoringBatteryOptimizations(context)) }
    val checklist = remember { ChecklistStore(context) }
    var zeroTierExempt by remember { mutableStateOf(checklist.zeroTierExempt()) }
    var oemDone by remember { mutableStateOf(checklist.oemSettingsDone()) }
    val oemGuide = remember { OemGuides.forManufacturer(Build.MANUFACTURER, Build.BRAND) }
    // ZeroTier matters only when this phone actually reaches the coordinator over it.
    val usesZeroTier = EngineConfigs.advertiseAddress(context, coordinatorUrl)?.isVpn == true &&
        NetworkStatusReader.read(context).zeroTierInstalled

    val limitStore = remember { LimitStore(context) }
    var limitBytes by remember { mutableLongStateOf(limitStore.limitBytes()) }
    val freeSpace = remember { StatFsFreeSpace(context.filesDir) }

    // Is the coordinator answering? Checked on open and on "Check again".
    var recheck by remember { mutableIntStateOf(0) }
    var reachable by remember { mutableStateOf<Boolean?>(null) }
    var reachProblem by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(coordinatorUrl, recheck) {
        if (coordinatorUrl.isBlank()) return@LaunchedEffect
        reachable = null
        when (val r = CoordinatorCheck.check(coordinatorUrl)) {
            is CheckResult.Reachable -> { reachable = true; reachProblem = null }
            is CheckResult.Failed -> { reachable = false; reachProblem = r.reason }
        }
    }

    // The user may change battery or notification settings outside the app; re-read them on return.
    LifecycleResumeEffect(Unit) {
        batteryExempt = isIgnoringBatteryOptimizations(context)
        notificationsGranted = hasNotificationPermission(context)
        limitBytes = limitStore.limitBytes()
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

    val range = StorageLimitPolicy.range(freeSpace.freeBytes())
    val registering = engineState is EngineState.Registering || engineState is EngineState.AwaitingOtp ||
        engineState is EngineState.Verifying
    val steps = SetupSteps.steps(
        SetupInputs(
            joined = coordinatorUrl.isNotBlank() && onboarding.phone().isNotBlank(),
            coordinatorReachable = reachable,
            coordinatorProblem = reachProblem,
            storageSaved = limitBytes > 0L,
            storageShortBy = (range as? LimitRange.NotEnoughSpace)?.let { formatGiB(it.requiredFreeBytes) },
            appBatteryExempt = batteryExempt,
            usesZeroTier = usesZeroTier,
            zeroTierExempt = zeroTierExempt,
            oemBrand = oemGuide?.brand,
            oemDone = oemDone,
            binaryAvailable = binaryAvailable,
            registered = registered,
            registering = registering,
            running = running,
            online = engineState == EngineState.Running,
        ),
    )
    var setupMessage by remember { mutableStateOf<String?>(null) }

    fun startProvider() {
        if (!notificationsGranted && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            notificationLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            ProviderController.start(context)
        }
    }

    fun act(action: StepAction) {
        setupMessage = null
        when (action) {
            StepAction.OPEN_NETWORK -> onOpenNetwork()
            StepAction.RECHECK -> recheck++
            StepAction.OPEN_STORAGE -> onOpenStorage()
            StepAction.USE_DEFAULT_STORAGE -> {
                val bytes = StorageLimitPolicy.clamp(BYTES_PER_GIB, range)
                if (bytes > 0L) {
                    limitStore.save(bytes)
                    limitBytes = bytes
                } else {
                    onOpenStorage()
                }
            }
            StepAction.BATTERY -> batteryLauncher.launch(
                Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${context.packageName}")),
            )
            StepAction.ZEROTIER -> openAppSettings(context, NetworkStatusReader.ZEROTIER_PACKAGE)
            StepAction.OEM -> oemGuide?.let { openWebPage(context, it.url) }
            StepAction.REGISTER -> when (val result = EngineConfigs.fromDevice(context, requireRegistered = false)) {
                is ConfigResult.Missing -> setupMessage = result.reason
                is ConfigResult.Ready -> ProviderEngine.startOnboarding(context, result.config)
            }
            StepAction.START -> startProvider()
            StepAction.NONE -> Unit
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ScreenTitle("Vyomanaut", onboarding.networkName().ifBlank { "Provider node" })

        SetupPanel(
            steps = steps,
            onAction = ::act,
            onConfirm = { action ->
                when (action) {
                    StepAction.ZEROTIER -> { zeroTierExempt = true; checklist.setZeroTierExempt(true) }
                    StepAction.OEM -> { oemDone = true; checklist.setOemSettingsDone(true) }
                    else -> Unit
                }
            },
            message = setupMessage,
        )

        RegistrationPanel(registered = registered, engineState = engineState)

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
            } else if (registered) {
                PixelButton(text = "Start provider", enabled = binaryAvailable && batteryExempt, onClick = ::startProvider)
            } else {
                Text("Follow \"Get connected\" above.", color = PixelColors.Muted)
            }
            if (!notificationsGranted) {
                Text("Notifications are off: the provider still runs, without a status notification.", color = PixelColors.Muted)
            }
        }

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

        PixelPanel(title = "Network profile: demo") {
            Text("Erasure code RS(${DemoProfile.DATA_SHARDS},${DemoProfile.TOTAL_SHARDS}), minimum ${DemoProfile.MIN_ACTIVE_PROVIDERS} providers")
            Text("Audit period ${DemoProfile.AUDIT_PERIOD_MINUTES} min, departure after ${DemoProfile.DEPARTURE_THRESHOLD_MINUTES} min offline")
            if (limitBytes > 0L) Text("You lend ${formatGiB(limitBytes)} (registered as ${ProviderArgs.storageGbFromLimit(limitBytes)} GB)")
            Text("Payments: see the Payments tab")
        }
    }
}

/** Code entry and reset. Starting registration is a step in "Get connected". */
@Composable
private fun RegistrationPanel(registered: Boolean, engineState: EngineState) {
    val context = LocalContext.current
    var otp by remember { mutableStateOf("") }
    var message by remember { mutableStateOf<String?>(null) }
    var confirmReset by remember { mutableStateOf(false) }

    val content: (@Composable () -> Unit)? = when {
        engineState is EngineState.Registering -> { { Text("Contacting the coordinator...") } }

        engineState is EngineState.AwaitingOtp -> { {
            Text("The coordinator sent a 6-digit code. Ask the host for it: it shows on their join desk.")
            PixelTextField(
                value = otp,
                onValueChange = { otp = it.filter(Char::isDigit).take(OTP_DIGITS) },
                label = "6-digit code",
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            )
            PixelButton(text = "Submit code", onClick = {
                message = if (ProviderEngine.submitOtp(otp)) null else "The code must be exactly 6 digits."
            })
        } }

        engineState is EngineState.Verifying -> { { Text("Verifying the code...") } }

        engineState is EngineState.NeedsRegistration -> { {
            Text(
                "Registering again deletes this phone's provider identity and stored shards, then runs " +
                    "onboarding with a new 6-digit code.",
            )
            PixelButton(text = "Register again", onClick = { ProviderEngine.resetRegistration(context) })
        } }

        registered -> { {
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
        } }

        else -> null
    }
    if (content == null) return
    PixelPanel(title = "Registration") {
        content()
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
