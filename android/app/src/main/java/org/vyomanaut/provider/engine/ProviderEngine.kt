package org.vyomanaut.provider.engine

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.File
import java.io.IOException

sealed interface EngineState {
    data object Idle : EngineState
    data object Registering : EngineState
    data object AwaitingOtp : EngineState
    data object Verifying : EngineState
    data object Running : EngineState
    data class Failed(val reason: String) : EngineState
}

/**
 * Runs the Go provider (bundled as libprovider.so) as a child process: `onboard` once, then `run`.
 * Same program, same flags as the Windows installer uses, so the phone behaves like a Windows provider.
 */
object ProviderEngine {
    const val OTP_PROMPT = "Enter the 6-digit code"
    const val BINARY_NAME = "libprovider.so"
    private const val MAX_LOG_LINES = 200
    private const val MSG_MISSING_BINARY =
        "Provider program is not bundled in this app. Build it with scripts/build_android_provider.ps1, then rebuild the app."

    private val mutableState = MutableStateFlow<EngineState>(EngineState.Idle)
    val state: StateFlow<EngineState> = mutableState.asStateFlow()

    private val mutableLog = MutableStateFlow<List<String>>(emptyList())
    val log: StateFlow<List<String>> = mutableLog.asStateFlow()

    private var process: ProviderProcess? = null
    private var generation = 0
    private var stopRequested = false
    private val pending = StringBuilder()

    // Android only lets an app run executables from its native library folder.
    fun binaryFile(context: Context): File = File(context.applicationInfo.nativeLibraryDir, BINARY_NAME)

    fun isBinaryAvailable(context: Context): Boolean = binaryFile(context).exists()

    @Synchronized
    fun startOnboarding(context: Context, config: EngineConfig) {
        if (launch(context, config, ProviderArgs.onboard(config), isOnboarding = true)) {
            mutableState.value = EngineState.Registering
        }
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

    /** Returns false (and sets a Failed state) when the program could not be started. */
    @Synchronized
    fun startRun(context: Context, config: EngineConfig): Boolean {
        val started = launch(context, config, ProviderArgs.run(config), isOnboarding = false)
        if (started) mutableState.value = EngineState.Running
        return started
    }

    @Synchronized
    fun fail(reason: String) {
        mutableState.value = EngineState.Failed(reason)
    }

    /** Stops the child process. A Failed state is kept so the reason stays visible. */
    @Synchronized
    fun shutdown() {
        generation++
        stopRequested = true
        process?.stop()
        process = null
        if (mutableState.value !is EngineState.Failed) mutableState.value = EngineState.Idle
    }

    private fun launch(context: Context, config: EngineConfig, args: List<String>, isOnboarding: Boolean): Boolean {
        process?.stop()
        process = null
        generation++
        val myGeneration = generation

        val binary = binaryFile(context)
        if (!binary.exists()) {
            mutableState.value = EngineState.Failed(MSG_MISSING_BINARY)
            return false
        }
        File(config.dataDir).mkdirs()
        mutableLog.value = emptyList()
        pending.setLength(0)
        stopRequested = false

        val store = EngineStore(context.applicationContext)
        val child = ProviderProcess(binary, context.filesDir, context.cacheDir)
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
