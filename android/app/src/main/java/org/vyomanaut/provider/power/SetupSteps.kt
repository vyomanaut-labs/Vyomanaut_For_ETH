package org.vyomanaut.provider.power

/** What the setup checklist on the Home screen knows about the phone right now. */
data class SetupInputs(
    /** A coordinator address and a phone number are saved (from the QR link or the Network tab). */
    val joined: Boolean,
    /** null = not checked yet, true = the coordinator answered, false = it did not. */
    val coordinatorReachable: Boolean?,
    val coordinatorProblem: String? = null,
    val storageSaved: Boolean,
    /** null = the phone has enough free space; otherwise how much it needs, for the message. */
    val storageShortBy: String? = null,
    val appBatteryExempt: Boolean,
    val usesZeroTier: Boolean,
    val zeroTierExempt: Boolean,
    /** The phone maker's name when it needs extra settings, else null. */
    val oemBrand: String?,
    val oemDone: Boolean,
    val binaryAvailable: Boolean,
    val registered: Boolean,
    val registering: Boolean,
    val running: Boolean,
    val online: Boolean,
)

enum class StepState { DONE, TODO, WAITING, OPTIONAL, BLOCKED }

/** What tapping a step's button does. */
enum class StepAction { OPEN_NETWORK, RECHECK, USE_DEFAULT_STORAGE, OPEN_STORAGE, BATTERY, ZEROTIER, OEM, REGISTER, START, NONE }

data class SetupStep(
    val title: String,
    val detail: String,
    val state: StepState,
    val action: StepAction = StepAction.NONE,
    val actionLabel: String = "",
    /** Recommended steps do not count towards "x of y done" and never block. */
    val required: Boolean = true,
)

/**
 * Pure rules for the "Get connected" checklist at the top of the Home screen: every requirement, in the order to
 * do them, with the one next step marked. No Android types, so it is unit-tested on the host.
 */
object SetupSteps {

    fun steps(i: SetupInputs): List<SetupStep> = buildList {
        add(
            if (i.joined) {
                SetupStep("Join a network", "Coordinator and phone number saved.", StepState.DONE)
            } else {
                SetupStep(
                    "Join a network",
                    "Scan the host's QR code and tap \"Open in the app\", or type the address on the Network tab.",
                    StepState.TODO, StepAction.OPEN_NETWORK, "Open Network tab",
                )
            },
        )
        add(
            when {
                !i.joined -> SetupStep("Reach the coordinator", "After joining.", StepState.BLOCKED)
                i.coordinatorReachable == true -> SetupStep("Reach the coordinator", "It answers.", StepState.DONE)
                i.coordinatorReachable == null -> SetupStep("Reach the coordinator", "Checking…", StepState.WAITING)
                else -> SetupStep(
                    "Reach the coordinator",
                    (i.coordinatorProblem ?: "No answer.") + " Check mobile data or Wi-Fi, and that the host is still running.",
                    StepState.TODO, StepAction.RECHECK, "Check again",
                )
            },
        )
        add(
            when {
                i.storageSaved -> SetupStep("Choose how much space to lend", "Saved.", StepState.DONE)
                i.storageShortBy != null -> SetupStep(
                    "Choose how much space to lend",
                    "This phone needs about ${i.storageShortBy} free. Free up space first.",
                    StepState.TODO, StepAction.OPEN_STORAGE, "Open Storage tab",
                )
                else -> SetupStep(
                    "Choose how much space to lend",
                    "1 GB is enough to take part. You can change it later on the Storage tab.",
                    StepState.TODO, StepAction.USE_DEFAULT_STORAGE, "Lend 1 GB",
                )
            },
        )
        add(
            if (i.appBatteryExempt) {
                SetupStep("Keep running in the background", "Battery optimisation is off for this app.", StepState.DONE)
            } else {
                SetupStep(
                    "Keep running in the background",
                    "Android stops apps on battery. Without this the phone drops off when the screen turns off.",
                    StepState.TODO, StepAction.BATTERY, "Allow",
                )
            },
        )
        if (i.usesZeroTier) {
            add(
                if (i.zeroTierExempt) {
                    SetupStep("Keep ZeroTier awake", "Done.", StepState.DONE)
                } else {
                    SetupStep(
                        "Keep ZeroTier awake",
                        "This phone reaches the network over ZeroTier. Open ZeroTier's settings, Battery, Unrestricted.",
                        StepState.TODO, StepAction.ZEROTIER, "Open ZeroTier settings",
                    )
                },
            )
        }
        if (i.oemBrand != null) {
            add(
                if (i.oemDone) {
                    SetupStep("${i.oemBrand} settings", "Done.", StepState.DONE, required = false)
                } else {
                    SetupStep(
                        "${i.oemBrand} settings (recommended)",
                        "${i.oemBrand} phones close background apps more than others. The guide shows the switches.",
                        StepState.OPTIONAL, StepAction.OEM, "Show me", required = false,
                    )
                },
            )
        }
        val readyToRegister = i.joined && i.coordinatorReachable == true && i.storageSaved && i.appBatteryExempt &&
            (!i.usesZeroTier || i.zeroTierExempt)
        add(
            when {
                !i.binaryAvailable -> SetupStep("Register this phone", "This build has no provider program.", StepState.BLOCKED)
                i.registered -> SetupStep("Register this phone", "Registered.", StepState.DONE)
                i.registering -> SetupStep(
                    "Register this phone",
                    "Ask the host for the 6-digit code on their join desk and type it below.",
                    StepState.WAITING,
                )
                !readyToRegister -> SetupStep("Register this phone", "After the steps above.", StepState.BLOCKED)
                else -> SetupStep(
                    "Register this phone",
                    "The host's join desk shows you a 6-digit code.",
                    StepState.TODO, StepAction.REGISTER, "Register",
                )
            },
        )
        add(
            when {
                i.online -> SetupStep("Go online", "Online. The coordinator now tests this phone for a few minutes before it stores real files.", StepState.DONE)
                i.running -> SetupStep("Go online", "Connecting to the relay and the coordinator…", StepState.WAITING)
                !i.registered -> SetupStep("Go online", "Starts by itself after registering.", StepState.BLOCKED)
                else -> SetupStep("Go online", "The provider is stopped.", StepState.TODO, StepAction.START, "Start")
            },
        )
    }

    /** The step to do now: the first one waiting on the user, else the first one waiting on the network. */
    fun next(steps: List<SetupStep>): SetupStep? =
        steps.firstOrNull { it.state == StepState.TODO } ?: steps.firstOrNull { it.state == StepState.WAITING }

    /** Required steps done, out of required steps (recommended ones are not counted). */
    fun progress(steps: List<SetupStep>): Pair<Int, Int> {
        val required = steps.filter { it.required }
        return required.count { it.state == StepState.DONE } to required.size
    }
}
