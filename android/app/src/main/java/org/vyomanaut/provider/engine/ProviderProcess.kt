package org.vyomanaut.provider.engine

import java.io.File
import java.io.IOException
import java.io.Writer
import kotlin.concurrent.thread

/** One run of the Go provider program as a child process. Output and errors arrive on one stream. */
class ProviderProcess(
    private val binary: File,
    private val workDir: File,
    private val tempDir: File,
) {
    private var process: Process? = null
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
        val started = builder.start()
        process = started
        stdin = started.outputStream.bufferedWriter()
        thread(name = "provider-output", isDaemon = true) {
            val reader = started.inputStream.reader()
            val buffer = CharArray(READ_BUFFER_CHARS)
            while (true) {
                val count = reader.read(buffer)
                if (count < 0) break
                onOutput(String(buffer, 0, count))
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

    fun stop() {
        process?.destroy()
    }

    private companion object {
        const val READ_BUFFER_CHARS = 512
    }
}
