package org.vyomanaut.provider.engine

import java.io.File
import java.io.IOException
import java.io.Writer
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

/** One run of the Go provider program as a child process. Output and errors arrive on one stream. */
class ProviderProcess(
    private val binary: File,
    private val workDir: File,
    private val tempDir: File,
    /** The phone's DNS servers: Android has no /etc/resolv.conf for the Go program to read. */
    private val dnsServers: List<String> = emptyList(),
) {
    @Volatile private var process: Process? = null
    private var stdin: Writer? = null

    /** Starts the program. [onOutput] gets raw chunks (a prompt may arrive without a newline). */
    @Throws(IOException::class)
    fun start(args: List<String>, onOutput: (String) -> Unit, onExit: (Int) -> Unit) {
        val builder = ProcessBuilder(listOf(binary.absolutePath) + args)
            .directory(workDir)
            .redirectErrorStream(true)
        // Android apps have no HOME; the Go program looks for one when building default paths.
        builder.environment()["HOME"] = workDir.absolutePath
        builder.environment()["TMPDIR"] = tempDir.absolutePath
        if (dnsServers.isNotEmpty()) builder.environment()["VYOMANAUT_DNS_SERVERS"] = dnsServers.joinToString(",")
        val started = builder.start()
        process = started
        stdin = started.outputStream.bufferedWriter()
        thread(name = "provider-output", isDaemon = true) {
            val reader = started.inputStream.reader()
            val buffer = CharArray(READ_BUFFER_CHARS)
            try {
                while (true) {
                    val count = reader.read(buffer)
                    if (count < 0) break
                    onOutput(String(buffer, 0, count))
                }
            } catch (e: IOException) {
                // The stream closes when the program is killed; the exit code below is what matters.
            }
            onExit(started.waitFor())
        }
    }

    fun writeLine(text: String) {
        try {
            stdin?.apply {
                write(text)
                write("\n")
                flush()
            }
        } catch (e: IOException) {
            // The program already exited; the exit handler reports the outcome.
        }
    }

    /**
     * Asks the program to stop (SIGTERM) and BLOCKS until it has exited, killing it if it does not exit in
     * time. The Go provider closes its Badger database and its listener on SIGTERM; a new run started before
     * that finishes fails with "address already in use" or "cannot acquire directory lock" (review issue 3b).
     * Never call this on the main thread. Returns true when the program is gone.
     */
    fun stopAndWait(): Boolean {
        val running = process ?: return true
        running.destroy()
        if (!running.waitFor(STOP_GRACE_SECONDS, TimeUnit.SECONDS)) {
            running.destroyForcibly()
            running.waitFor(KILL_WAIT_SECONDS, TimeUnit.SECONDS)
        }
        return !running.isAlive
    }

    private companion object {
        const val READ_BUFFER_CHARS = 512
        const val STOP_GRACE_SECONDS = 5L
        const val KILL_WAIT_SECONDS = 2L
    }
}
