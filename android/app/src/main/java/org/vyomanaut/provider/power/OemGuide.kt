package org.vyomanaut.provider.power

/** Extra battery settings a phone maker adds on top of Android's own Doze, and where to read more. */
data class OemGuide(
    val brand: String,
    val steps: String,
    val url: String,
)

/**
 * Pure rules: which phone makers kill background apps beyond stock Android, and what to switch off.
 * Steps follow dontkillmyapp.com; menu names differ slightly between OS versions.
 */
object OemGuides {
    private const val SITE = "https://dontkillmyapp.com/"

    private val guides = listOf(
        Entry(
            listOf("xiaomi", "redmi", "poco"),
            OemGuide(
                "Xiaomi / Redmi / POCO",
                "Settings > Apps > Manage apps > Vyomanaut Provider: turn on Autostart, set Battery saver to " +
                    "No restrictions. Do the same for ZeroTier One. In Recents, long-press both apps and lock them.",
                SITE + "xiaomi",
            ),
        ),
        Entry(
            listOf("oppo"),
            OemGuide(
                "OPPO",
                "Settings > Battery > App battery management: allow background activity and auto launch for " +
                    "Vyomanaut Provider and ZeroTier One. Lock both apps in Recents.",
                SITE + "oppo",
            ),
        ),
        Entry(
            listOf("realme"),
            OemGuide(
                "realme",
                "Settings > Battery > App battery management: allow background activity and auto launch for " +
                    "Vyomanaut Provider and ZeroTier One. Lock both apps in Recents.",
                SITE + "realme",
            ),
        ),
        Entry(
            listOf("vivo", "iqoo"),
            OemGuide(
                "vivo / iQOO",
                "Settings > Battery > Background power consumption management: allow high background power use " +
                    "for Vyomanaut Provider and ZeroTier One. Also allow Autostart for both.",
                SITE + "vivo",
            ),
        ),
        Entry(
            listOf("samsung"),
            OemGuide(
                "Samsung",
                "Settings > Battery > Background usage limits: make sure neither Vyomanaut Provider nor ZeroTier " +
                    "One is in Sleeping or Deep sleeping apps, and add both to Never sleeping apps.",
                SITE + "samsung",
            ),
        ),
        Entry(
            listOf("oneplus"),
            OemGuide(
                "OnePlus",
                "Settings > Battery > Battery optimisation: set both apps to Don't optimise, and turn off " +
                    "Advanced optimisation / Sleep standby optimisation. Lock both apps in Recents.",
                SITE + "oneplus",
            ),
        ),
        Entry(
            listOf("huawei"),
            OemGuide(
                "Huawei",
                "Settings > Battery > App launch: set both apps to Manage manually and allow auto-launch, " +
                    "secondary launch and run in background.",
                SITE + "huawei",
            ),
        ),
        Entry(
            listOf("honor"),
            OemGuide(
                "Honor",
                "Settings > Battery > App launch: set both apps to Manage manually and allow auto-launch, " +
                    "secondary launch and run in background.",
                SITE + "honor",
            ),
        ),
    )

    /** Returns the guide for this phone maker, or null when stock Android battery settings are enough. */
    fun forManufacturer(manufacturer: String, brand: String = ""): OemGuide? {
        val names = listOf(manufacturer, brand).map { it.trim().lowercase() }.filter { it.isNotEmpty() }
        return guides.firstOrNull { entry -> names.any { name -> entry.keys.any { name.contains(it) } } }?.guide
    }

    private data class Entry(val keys: List<String>, val guide: OemGuide)
}
