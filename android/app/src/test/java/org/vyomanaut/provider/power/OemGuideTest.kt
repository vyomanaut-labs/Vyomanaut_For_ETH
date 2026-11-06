package org.vyomanaut.provider.power

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class OemGuideTest {

    @Test
    fun xiaomiFamilyMapsToXiaomiGuide() {
        for (name in listOf("Xiaomi", "Redmi", "POCO")) {
            assertEquals("https://dontkillmyapp.com/xiaomi", OemGuides.forManufacturer(name)?.url)
        }
    }

    @Test
    fun brandIsCheckedWhenManufacturerIsGeneric() {
        assertEquals("https://dontkillmyapp.com/realme", OemGuides.forManufacturer("unknown", "realme")?.url)
    }

    @Test
    fun matchingIgnoresCaseAndSpaces() {
        assertNotNull(OemGuides.forManufacturer("  SAMSUNG "))
    }

    @Test
    fun stockPhonesNeedNoGuide() {
        assertNull(OemGuides.forManufacturer("Google", "google"))
        assertNull(OemGuides.forManufacturer("", ""))
    }

    @Test
    fun honorIsNotTreatedAsHuawei() {
        assertEquals("Honor", OemGuides.forManufacturer("HONOR")?.brand)
    }
}
