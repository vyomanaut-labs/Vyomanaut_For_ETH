package org.vyomanaut.provider.engine

/** Why the coordinator no longer accepts this phone's saved registration. */
enum class Rejection(val message: String) {
    /** The coordinator marked this provider departed (offline longer than the departure threshold). */
    DEPARTED(
        "The coordinator marked this phone as departed (it was offline too long). " +
            "Register again to rejoin the network.",
    ),

    /** The coordinator does not know this provider: usually it was restarted with a fresh database. */
    UNKNOWN_TO_COORDINATOR(
        "The coordinator no longer recognises this phone's registration (usually because the coordinator " +
            "was restarted). Register again to rejoin.",
    ),
}

/**
 * Pure rules that read the Go provider's output (review issue 3c). The provider does not exit when the
 * coordinator rejects it; it keeps retrying its heartbeat forever, so the app has to spot these lines.
 * Matched texts come from internal/p2p/heartbeat.go and the coordinator's error codes.
 */
object ProviderLogRules {
    private const val DEPARTED_CODE = "PROVIDER_DEPARTED"
    private const val MISMATCH_TEXT = "saved registration does not match the coordinator"
    private const val PHONE_TAKEN_CODE = "PHONE_ALREADY_REGISTERED"

    /**
     * A departed reply is definite, so one is enough. The 401 explanation is printed once per heartbeat and
     * a single one can be a coordinator restart in progress, so wait for a second one.
     */
    const val MISMATCH_LINES_TO_ACT = 2

    const val PHONE_TAKEN_MESSAGE =
        "This phone number is already registered with the coordinator. If it was marked departed, register " +
            "with a different number or ask the network operator to remove the old registration."

    fun rejectionIn(line: String): Rejection? = when {
        line.contains(DEPARTED_CODE) -> Rejection.DEPARTED
        line.contains(MISMATCH_TEXT) -> Rejection.UNKNOWN_TO_COORDINATOR
        else -> null
    }

    fun isPhoneTaken(line: String): Boolean = line.contains(PHONE_TAKEN_CODE)

    /** [linesSeen] counts how many times [rejection] has appeared in this run so far, this line included. */
    fun shouldAct(rejection: Rejection, linesSeen: Int): Boolean = when (rejection) {
        Rejection.DEPARTED -> linesSeen >= 1
        Rejection.UNKNOWN_TO_COORDINATOR -> linesSeen >= MISMATCH_LINES_TO_ACT
    }
}
