package org.vyomanaut.provider.payments

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PaymentsDefaultsTest {

    @Test
    fun chainApiIsTheCoordinatorHostOnPort8090() {
        assertEquals("http://172.27.1.5:8090", PaymentsDefaults.chainApiFor("http://172.27.1.5:8080"))
        assertEquals("https://coord.example:8090", PaymentsDefaults.chainApiFor("https://coord.example"))
    }

    @Test
    fun unusableCoordinatorGivesNoDefault() {
        assertNull(PaymentsDefaults.chainApiFor(""))
        assertNull(PaymentsDefaults.chainApiFor("not a url"))
    }
}
