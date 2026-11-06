package org.vyomanaut.provider.network

/** An IPv4 address of this phone, its subnet prefix length, and whether it is on a VPN (ZeroTier) network. */
data class NetAddress(val address: String, val prefixLength: Int, val isVpn: Boolean = true)

/**
 * Pure rule for which of the phone's addresses to advertise to the network.
 *
 * The coordinator and the uploading clients must be able to open connections TO this address. Prefer the address
 * whose subnet contains the coordinator, whether that is the ZeroTier network or a shared Wi-Fi/LAN. Taking the
 * first VPN address is wrong when another VPN is on, and on Android 14-16 phones incoming connections over a VPN
 * are not delivered to apps at all (found on test devices), so a shared Wi-Fi is the reliable setup.
 * Fallbacks when nothing matches (coordinator given by name): the first VPN address, then the first other one.
 */
object AddressPicker {
    private const val IPV4_BITS = 32
    private const val OCTETS = 4
    private const val OCTET_MAX = 255
    private const val BITS_PER_OCTET = 8

    fun pick(candidates: List<NetAddress>, coordinatorHost: String?): NetAddress? {
        if (candidates.isEmpty()) return null
        val coordinator = coordinatorHost?.let(::parseIpv4)
        if (coordinator != null) {
            candidates.firstOrNull { sameSubnet(it, coordinator) }?.let { return it }
        }
        return candidates.firstOrNull { it.isVpn } ?: candidates.first()
    }

    private fun sameSubnet(candidate: NetAddress, other: Long): Boolean {
        val own = parseIpv4(candidate.address) ?: return false
        val prefix = candidate.prefixLength
        if (prefix !in 1..IPV4_BITS) return false
        val mask = (0xFFFFFFFFL shl (IPV4_BITS - prefix)) and 0xFFFFFFFFL
        return (own and mask) == (other and mask)
    }

    /** Parses a dotted IPv4 literal; returns null for host names and anything malformed. */
    fun parseIpv4(text: String): Long? {
        val parts = text.trim().split('.')
        if (parts.size != OCTETS) return null
        var value = 0L
        for (part in parts) {
            if (part.isEmpty() || part.length > 3 || !part.all(Char::isDigit)) return null
            val octet = part.toInt()
            if (octet > OCTET_MAX) return null
            value = (value shl BITS_PER_OCTET) or octet.toLong()
        }
        return value
    }
}
