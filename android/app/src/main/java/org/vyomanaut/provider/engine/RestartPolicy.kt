package org.vyomanaut.provider.engine

/**
 * Pure rules for restarting the provider after it exits on its own (review issue 3a).
 *
 * Waits grow 5 s, 15 s, 30 s, 60 s, then stay at 120 s. A run that stayed up for [STABLE_AFTER_MS] counts as
 * healthy, so the next failure starts again from the first step. After [MAX_ATTEMPTS] failures in a row the
 * app stops trying and tells the user, instead of looping forever behind an "online" label.
 */
object RestartPolicy {
    private val delaysSeconds = longArrayOf(5, 15, 30, 60, 120)
    const val MAX_ATTEMPTS = 10
    const val STABLE_AFTER_MS = 5L * 60 * 1000

    /** The attempt number for the restart after a run that lasted [uptimeMs]; the first restart is 1. */
    fun nextAttempt(previousAttempt: Int, uptimeMs: Long): Int =
        if (uptimeMs >= STABLE_AFTER_MS) 1 else previousAttempt + 1

    fun shouldGiveUp(attempt: Int): Boolean = attempt > MAX_ATTEMPTS

    /** Seconds to wait before restart [attempt] (1-based). */
    fun delaySeconds(attempt: Int): Long = delaysSeconds[(attempt - 1).coerceIn(0, delaysSeconds.lastIndex)]
}
