package org.vyomanaut.provider.storage

import java.util.Locale

/** Display only: gigabytes (binary) with one decimal. */
fun formatGiB(bytes: Long): String =
    String.format(Locale.US, "%.1f GB", bytes.toDouble() / BYTES_PER_GIB)
