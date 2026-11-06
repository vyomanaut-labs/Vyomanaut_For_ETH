package org.vyomanaut.provider.engine

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.File
import java.io.IOException
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService

sealed interface EngineState {
    data object Idle : EngineState
    data object Registering : EngineState
    data object AwaitingOtp : EngineState
    data object Verifying : EngineState
    data object Starting : EngineState
    data object Running : EngineState
    data class Failed(val reason: String) : EngineState
}

/**
 * Runs the Go provider (bundled as libprovider.so) as a child process: `onboard` once, then `run`.
 * Same program, same flags as the Windows installer uses, so the phone behaves like a Windows provider.
 *
 * Starting and stopping happen on one background thread, in order: stopping waits for the old program
 * to exit, so it must never run on the main thread, and a new program never starts before the old one
 * has released its port and database.
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

    // Android only lets an app run executables from its native library folder.
    fun binaryFile(context: Context): File = File(context.applicationInfo.nativeLibraryDir, BINARY_NAME)

    fun isBinaryAvailable(context: Context): Boolean = binaryFile(context).exists()

    fun startOnboarding(context: Context, config: EngineConfig) {
        val appContext = context.applicationContext
        mutableState.value = EngineState.Registering
        worker.execute { launch(appContext, config, ProviderArgs.onboard(config), isOnboarding = true) }
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

    /** Starts `provider run` in the background. The outcome shows up in [state]. */
    fun startRun(context: Context, config: EngineConfig) {
        val appContext = context.applicationContext
        mutableState.value = EngineState.Starting
        worker.execute {
            if (launch(appContext, config, ProviderArgs.run(config), isOnboarding = false)) {
                mutableState.value = EngineState.Running
            }
        }
    }

    @Synchronized
    fun fail(reason: String) {
        mutableState.value = EngineState.Failed(reason)
    }

    /** Stops the child process in the background. A Failed state is kept so the reason stays visible. */
    fun shutdown() {
        worker.execute {
            stopCurrent()
            synchronized(this) {
                if (mutableState.value !is EngineState.Failed) mutableState.value = EngineState.Idle
            }
        }
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
            mutableLog.value = emptyList()
            pending.setLength(0)
            stopRequested = false
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
        if (mutableState.value == EngineState.Registering && pending.contains(OTP_PROMPT)) {
            mutableState.value = EngineState.AwaitingOtp
        }
    }

    @Synchronized
    private fun handleExit(forGeneration: Int, store: EngineStore, config: EngineConfig, isOnboarding: Boolean, code: Int) {
        if (forGeneration != generation) return
        process = null
        mutableState.value = when {
            stopRequested -> EngineState.Idle
            isOnboarding && code == 0 -> {
                store.markRegistered(config.coordinatorUrl)
                EngineState.Idle
            }
            isOnboarding -> EngineState.Failed("Registration failed (exit code $code). See the details below.")
            else -> EngineState.Failed("Provider stopped unexpectedly (exit code $code). See the details below.")
        }
    }
}
