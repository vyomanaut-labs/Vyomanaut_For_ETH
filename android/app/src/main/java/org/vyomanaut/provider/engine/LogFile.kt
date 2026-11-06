package org.vyomanaut.provider.engine

import java.io.File
import java.io.IOException

/**
 * The last provider output lines, kept on disk so they survive the app being killed and restarted
 * (START_STICKY restarts the service with a fresh process and empty memory). Writes go to a temporary
 * file first and are renamed into place, so a crash mid-write never leaves half a file.
 */
object LogFile {
    const val NAME = "provider-log.txt"

    fun read(file: File, maxLines: Int): List<String> = try {
        if (file.exists()) file.readLines().takeLast(maxLines) else emptyList()
    } catch (e: IOException) {
        emptyList()
    }

    fun write(file: File, lines: List<String>) {
        try {
            val temp = File(file.parentFile, file.name + ".tmp")
            temp.writeText(lines.joinToString("\n", postfix = if (lines.isEmpty()) "" else "\n"))
            if (!temp.renameTo(file)) {
                file.delete()
                temp.renameTo(file)
            }
        } catch (e: IOException) {
            // Losing the saved copy of a log is not worth failing the provider over.
        }
    }
}
