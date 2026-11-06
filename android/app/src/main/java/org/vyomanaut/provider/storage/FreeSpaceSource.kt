package org.vyomanaut.provider.storage

import android.os.StatFs
import java.io.File

interface FreeSpaceSource {
    fun freeBytes(): Long
    fun totalBytes(): Long
}

/** Reads the volume that holds the app's private files, which is where shards will be stored. */
class StatFsFreeSpace(private val directory: File) : FreeSpaceSource {
    override fun freeBytes(): Long = StatFs(directory.path).availableBytes
    override fun totalBytes(): Long = StatFs(directory.path).totalBytes
}
