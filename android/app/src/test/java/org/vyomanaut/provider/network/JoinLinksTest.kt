package org.vyomanaut.provider.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class JoinLinksTest {
    @Test
    fun parsesTheJoinPageLink() {
        val link = JoinLinks.parse(
            "vyomanaut://join?chain=https%3A%2F%2Fchain.example&coordinator=https%3A%2F%2Fabc.trycloudflare.com" +
                "&name=ETHGlobal+demo&relay=bore.pub%3A41234",
        )!!
        assertEquals("https://abc.trycloudflare.com", link.coordinatorUrl)
        assertEquals("bore.pub:41234", link.relay)
        assertEquals("https://chain.example", link.chainApiUrl)
        assertEquals("ETHGlobal demo", link.networkName)
    }

    @Test
    fun takesTheFirstUsableRelay() {
        val link = JoinLinks.parse("vyomanaut://join?coordinator=http%3A%2F%2F10.0.0.2%3A8080&relay=nonsense,10.0.0.2%3A30300")!!
        assertEquals("10.0.0.2:30300", link.relay)
        assertNull(link.chainApiUrl)
        assertEquals(JoinLinks.DEFAULT_NAME, link.networkName)
    }

    @Test
    fun rejectsOtherLinks() {
        assertNull(JoinLinks.parse("https://example.com/join?coordinator=http%3A%2F%2Fx"))
        assertNull(JoinLinks.parse("vyomanaut://other?coordinator=http%3A%2F%2Fx"))
        assertNull(JoinLinks.parse("vyomanaut://join?coordinator=javascript%3Aalert(1)"))
        assertNull(JoinLinks.parse("vyomanaut://join"))
    }

    @Test
    fun readsNetworkInfo() {
        val json = """{"network_name":"Lab","coordinator_url":"https://c.example","relay_addrs":["bore.pub:1"],
            |"chain_api_url":"","join_url":"https://c.example/join"}""".trimMargin()
        val link = JoinLinks.fromInfo(json)!!
        assertEquals("https://c.example", link.coordinatorUrl)
        assertEquals("bore.pub:1", link.relay)
        assertNull(link.chainApiUrl)
        assertEquals("Lab", link.networkName)
        assertNull(JoinLinks.fromInfo("not json"))
    }

    @Test
    fun stripsOddCharactersFromTheName() {
        val link = JoinLinks.parse("vyomanaut://join?coordinator=http%3A%2F%2Fx.example&name=%3Cb%3EHi%3C%2Fb%3E")!!
        assertEquals("bHib", link.networkName)
    }
}
