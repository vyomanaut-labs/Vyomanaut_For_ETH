package org.vyomanaut.provider.network

/** An IPv4 address on a VPN network, with the length of its subnet prefix. */
data class VpnAddress(val address: String, val prefixLength: Int)

/**
 * Pure rule for which VPN address to advertise to the network.
 *
 * Taking the first VPN IPv4 is wrong when another VPN is on, so prefer the address whose subnet contains the
 * coordinator: the coordinator is reached over ZeroTier, so that subnet is the ZeroTier network. When the
 * coordinator is given by name, or no subnet matches, fall back to the first address.
 */
object AddressPicker {
    private const val IPV4_BITS = 32
    private const val OCTETS = 4
    private const val OCTET_MAX = 255
    private const val BITS_PER_OCTET = 8

    fun pick(candidates: List<VpnAddress>, coordinatorHost: String?): String? {
        if (candidates.isEmpty()) return null
        val coordinator = coordinatorHost?.let(::parseIpv4)
        if (coordinator != null) {
            candidates.firstOrNull { sameSubnet(it, coordinator) }?.let { return it.address }
        }
        return candidates.first().address
    }

    private fun sameSubnet(candidate: VpnAddress, other: Long): Boolean {
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
