package org.vyomanaut.provider.engine

import android.content.Context
import android.os.SystemClock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.File
import java.io.IOException
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

sealed interface EngineState {
    data object Idle : EngineState
    data object Registering : EngineState
    data object AwaitingOtp : EngineState
    data object Verifying : EngineState
    data object Starting : EngineState
    data object Running : EngineState

    /** The provider exited on its own; the app restarts it after [delaySeconds]. */
    data class Restarting(val attempt: Int, val delaySeconds: Long, val reason: String) : EngineState

    /** The coordinator rejected the saved registration; the phone must register again. */
    data class NeedsRegistration(val reason: String) : EngineState
    data class Failed(val reason: String) : EngineState
}

/** True while the provider is meant to be running: started, running, or waiting to restart. */
val EngineState.isActive: Boolean
    get() = this is EngineState.Starting || this is EngineState.Running || this is EngineState.Restarting

/**
 * Runs the Go provider (bundled as libprovider.so) as a child process: `onboard` once, then `run`.
 * Same program, same flags as the Windows installer uses, so the phone behaves like a Windows provider.
 *
 * Starting and stopping happen on one background thread, in order: stopping waits for the old program
 * to exit, so it must never run on the main thread, and a new program never starts before the old one
 * has released its port and database.
 *
 * While running, the engine supervises the provider: if it exits on its own it is restarted with growing
 * waits ([RestartPolicy]), and each restart rebuilds the configuration so a changed ZeroTier address is
 * picked up.
 */
object ProviderEngine {
    const val OTP_PROMPT = "Enter the 6-digit code"
    const val BINARY_NAME = "libprovider.so"
    private const val MAX_LOG_LINES = 200
    private const val MSG_MISSING_BINARY =
        "Provider program is not bundled in this app. Build it with scripts/build_android_provider.ps1, then rebuild the app."
    private const val MSG_STUCK =
        "The previous provider program did not exit. Restart the phone if this keeps happening."

    private val mutableState = MutableStateFlow<EngineState>(EngineState.Idle)
    val state: StateFlow<EngineState> = mutableState.asStateFlow()

    private val mutableLog = MutableStateFlow<List<String>>(emptyList())
    val log: StateFlow<List<String>> = mutableLog.asStateFlow()

    private val worker: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor { task ->
        Thread(task, "provider-engine").apply { isDaemon = true }
    }

    private var process: ProviderProcess? = null
    private var generation = 0
    private var stopRequested = false
    private val pending = StringBuilder()

    // Supervision of `provider run`.
    private var appContext: Context? = null
    private var keepRunning = false
    private var attempt = 0
    private var runStartedAt = 0L
    private var scheduledRestart: ScheduledFuture<*>? = null

    // Per-program output checks (ProviderLogRules).
    private var currentIsOnboarding = false
    private val rejectionsSeen = mutableMapOf<Rejection, Int>()
    private var phoneTaken = false

    // Android only lets an app run executables from its native library folder.
    fun binaryFile(context: Context): File = File(context.applicationInfo.nativeLibraryDir, BINARY_NAME)

    fun isBinaryAvailable(context: Context): Boolean = binaryFile(context).exists()

    fun startOnboarding(context: Context, config: EngineConfig) {
        val ctx = context.applicationContext
        synchronized(this) {
            keepRunning = false
            cancelScheduledRestart()
            mutableLog.value = emptyList()
            mutableState.value = EngineState.Registering
        }
        worker.execute { launch(ctx, config, ProviderArgs.onboard(config), isOnboarding = true) }
    }

    /** Sends the 6-digit code to the waiting onboarding program. Returns false if the code is malformed. */
    @Synchronized
    fun submitOtp(code: String): Boolean {
        val trimmed = code.trim()
        if (!ProviderArgs.isValidOtp(trimmed)) return false
        pending.setLength(0)
        mutableState.value = EngineState.Verifying
        process?.writeLine(trimmed)
        return true
    }

    /**
     * Starts and supervises `provider run` in the background. The outcome shows up in [state]: Running,
     * Restarting while it recovers from an exit, or Failed when it cannot start or gave up.
     */
    fun startRun(context: Context) {
        synchronized(this) {
            appContext = context.applicationContext
            keepRunning = true
            attempt = 0
            cancelScheduledRestart()
            mutableLog.value = emptyList()
            mutableState.value = EngineState.Starting
        }
        worker.execute { runOnce() }
    }

    /** Skips the remaining wait when the provider is waiting to restart (for example, the network came back). */
    fun retryNow(reason: String) {
        synchronized(this) {
            if (!keepRunning || mutableState.value !is EngineState.Restarting) return
            cancelScheduledRestart()
            appendLog("[APP] $reason: restarting now")
        }
        worker.execute { runOnce() }
    }

    @Synchronized
    fun fail(reason: String) {
        mutableState.value = EngineState.Failed(reason)
    }

    /** Stops the provider in the background and ends supervision. A Failed state is kept so the reason stays visible. */
    fun shutdown() {
        synchronized(this) {
            keepRunning = false
            cancelScheduledRestart()
        }
        worker.execute {
            stopCurrent()
            synchronized(this) {
                if (mutableState.value !is EngineState.Failed) mutableState.value = EngineState.Idle
            }
        }
    }

    /**
     * Stops the provider, deletes its data directory (identity, registration and shards) and forgets the
     * registration, so the phone can onboard again from scratch.
     */
    fun resetRegistration(context: Context) {
        val ctx = context.applicationContext
        synchronized(this) {
            keepRunning = false
            cancelScheduledRestart()
        }
        worker.execute {
            stopCurrent()
            EngineConfigs.dataDir(ctx).deleteRecursively()
            EngineStore(ctx).clearRegistration()
            synchronized(this) {
                mutableLog.value = emptyList()
                appendLog("[APP] Registration reset. Register this phone again.")
                mutableState.value = EngineState.Idle
            }
        }
    }

    /** The coordinator will not take this registration back; stop retrying and send the user to onboarding. */
    @Synchronized
    private fun onRejected(rejection: Rejection) {
        if (!keepRunning) return
        keepRunning = false
        cancelScheduledRestart()
        appendLog("[APP] ${rejection.message}")
        mutableState.value = EngineState.NeedsRegistration(rejection.message)
        appContext?.let { EngineStore(it).clearRegistration() }
        worker.execute { stopCurrent() }
    }

    /** Worker thread: builds a fresh configuration (the ZeroTier address may have changed) and launches. */
    private fun runOnce() {
        val ctx = synchronized(this) { if (keepRunning) appContext else null } ?: return
        when (val result = EngineConfigs.fromDevice(ctx, requireRegistered = true)) {
            is ConfigResult.Missing ->
                if (result.retryable) scheduleRestart(result.reason, uptimeMs = 0) else giveUp(result.reason)

            is ConfigResult.Ready ->
                if (launch(ctx, result.config, ProviderArgs.run(result.config), isOnboarding = false)) {
                    synchronized(this) {
                        runStartedAt = SystemClock.elapsedRealtime()
                        if (keepRunning) mutableState.value = EngineState.Running
                    }
                } else {
                    giveUp((mutableState.value as? EngineState.Failed)?.reason ?: "The provider could not start.")
                }
        }
    }

    @Synchronized
    private fun scheduleRestart(reason: String, uptimeMs: Long) {
        if (!keepRunning) return
        attempt = RestartPolicy.nextAttempt(attempt, uptimeMs)
        if (RestartPolicy.shouldGiveUp(attempt)) {
            giveUp("The provider stopped ${RestartPolicy.MAX_ATTEMPTS} times in a row. Last problem: $reason")
            return
        }
        val delay = RestartPolicy.delaySeconds(attempt)
        appendLog("[APP] $reason. Restart ${attempt}/${RestartPolicy.MAX_ATTEMPTS} in $delay s.")
        mutableState.value = EngineState.Restarting(attempt, delay, reason)
        cancelScheduledRestart()
        scheduledRestart = worker.schedule({ runOnce() }, delay, TimeUnit.SECONDS)
    }

    @Synchronized
    private fun giveUp(reason: String) {
        keepRunning = false
        cancelScheduledRestart()
        mutableState.value = EngineState.Failed(reason)
    }

    private fun cancelScheduledRestart() {
        scheduledRestart?.cancel(false)
        scheduledRestart = null
    }

    @Synchronized
    private fun appendLog(line: String) {
        mutableLog.value = (mutableLog.value + line).takeLast(MAX_LOG_LINES)
    }

    /** Detaches the current program (its output and exit are ignored from now on) and waits for it to exit. */
    private fun stopCurrent(): Boolean {
        val old = synchronized(this) {
            generation++
            stopRequested = true
            process.also { process = null }
        }
        return old?.stopAndWait() ?: true
    }

    /** Runs on the worker thread only. */
    private fun launch(context: Context, config: EngineConfig, args: List<String>, isOnboarding: Boolean): Boolean {
        if (!stopCurrent()) {
            fail(MSG_STUCK)
            return false
        }
        val binary = binaryFile(context)
        if (!binary.exists()) {
            fail(MSG_MISSING_BINARY)
            return false
        }
        File(config.dataDir).mkdirs()
        val store = EngineStore(context)
        val child = ProviderProcess(binary, context.filesDir, context.cacheDir)
        synchronized(this) {
            val myGeneration = generation
            pending.setLength(0)
            stopRequested = false
            currentIsOnboarding = isOnboarding
            rejectionsSeen.clear()
            phoneTaken = false
            try {
                child.start(
                    args,
                    onOutput = { chunk -> handleOutput(myGeneration, chunk) },
                    onExit = { code -> handleExit(myGeneration, store, config, isOnboarding, code) },
                )
            } catch (e: IOException) {
                mutableState.value = EngineState.Failed("Cannot start the provider program: ${e.message}")
                return false
            }
            process = child
        }
        return true
    }

    @Synchronized
    private fun handleOutput(forGeneration: Int, chunk: String) {
        if (forGeneration != generation) return
        pending.append(chunk.replace("\r", ""))
        val parts = pending.toString().split('\n')
        val complete = parts.dropLast(1)
        val partial = parts.last()
        pending.setLength(0)
        pending.append(partial)
        if (complete.isNotEmpty()) {
            mutableLog.value = (mutableLog.value + complete).takeLast(MAX_LOG_LINES)
        }
        for (line in complete) checkLine(line)
        if (mutableState.value == EngineState.Registering && pending.contains(OTP_PROMPT)) {
            mutableState.value = EngineState.AwaitingOtp
        }
    }

    @Synchronized
    private fun checkLine(line: String) {
        if (currentIsOnboarding) {
            if (ProviderLogRules.isPhoneTaken(line)) phoneTaken = true
            return
        }
        val rejection = ProviderLogRules.rejectionIn(line) ?: return
        val seen = (rejectionsSeen[rejection] ?: 0) + 1
        rejectionsSeen[rejection] = seen
        if (ProviderLogRules.shouldAct(rejection, seen)) onRejected(rejection)
    }

    @Synchronized
    private fun handleExit(forGeneration: Int, store: EngineStore, config: EngineConfig, isOnboarding: Boolean, code: Int) {
        if (forGeneration != generation) return
        process = null
        when {
            stopRequested -> mutableState.value = EngineState.Idle
            isOnboarding && code == 0 -> {
                store.markRegistered(config.coordinatorUrl)
                mutableState.value = EngineState.Idle
            }
            isOnboarding && phoneTaken -> mutableState.value = EngineState.Failed(ProviderLogRules.PHONE_TAKEN_MESSAGE)
            isOnboarding -> mutableState.value =
                EngineState.Failed("Registration failed (exit code $code). See the details below.")
            keepRunning -> scheduleRestart(
                "Provider stopped unexpectedly (exit code $code)",
                uptimeMs = SystemClock.elapsedRealtime() - runStartedAt,
            )
            else -> mutableState.value =
                EngineState.Failed("Provider stopped unexpectedly (exit code $code). See the details below.")
        }
    }
}
