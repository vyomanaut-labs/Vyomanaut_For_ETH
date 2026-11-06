package org.vyomanaut.provider.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import org.vyomanaut.provider.network.AddressPicker
import org.vyomanaut.provider.network.CheckResult
import org.vyomanaut.provider.network.CoordinatorCheck
import org.vyomanaut.provider.network.NetworkRules
import org.vyomanaut.provider.network.NetworkStatus
import org.vyomanaut.provider.network.RelayRules
import org.vyomanaut.provider.network.NetworkStatusReader
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel
import org.vyomanaut.provider.ui.pixel.PixelTextField
import org.vyomanaut.provider.ui.pixel.ScreenTitle
import kotlinx.coroutines.launch

@Composable
fun NetworkScreen() {
    val context = LocalContext.current
    val store = remember { OnboardingStore(context) }
    val scope = rememberCoroutineScope()

    var status by remember { mutableStateOf(NetworkStatusReader.read(context)) }
    var urlText by remember { mutableStateOf(store.coordinatorUrl()) }
    var phoneText by remember { mutableStateOf(store.phone()) }
    var relayText by remember { mutableStateOf(store.savedRelay()) }
    var inputError by remember { mutableStateOf<String?>(null) }
    var checking by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<CheckResult?>(null) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ScreenTitle("Network", "Link to the coordinator")

        PixelPanel(title = "ZeroTier") {
            Text(
                if (status.zeroTierInstalled) "ZeroTier app: installed" else "ZeroTier app: not installed",
                color = if (status.zeroTierInstalled) PixelColors.Good else PixelColors.Bad,
            )
            Text(
                if (status.vpnActive) "VPN: connected" else "VPN: not connected. Open ZeroTier and turn your network on.",
                color = if (status.vpnActive) PixelColors.Good else PixelColors.Bad,
            )
            if (status.vpnAddresses.isNotEmpty()) {
                Text("Your ZeroTier address: ${status.vpnAddresses.joinToString()}")
            }
            Text(
                "Android allows one VPN at a time, so no other VPN can run while you provide storage.",
                color = PixelColors.Muted,
            )
            PixelButton(text = "Refresh", color = PixelColors.PanelLight, onClick = { status = NetworkStatusReader.read(context) })
            if (status.zeroTierInstalled) {
                PixelButton(text = "Open ZeroTier", color = PixelColors.PanelLight, onClick = {
                    context.packageManager.getLaunchIntentForPackage(NetworkStatusReader.ZEROTIER_PACKAGE)
                        ?.let { context.startActivity(it) }
                })
            }
        }

        AdvertisedAddressPanel(status, store.coordinatorUrl(), store.relayAddr())

        PixelPanel(title = "Coordinator") {
            PixelTextField(
                value = urlText,
                onValueChange = { urlText = it },
                label = "Coordinator address",
                placeholder = "http://10.35.114.52:8080",
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
            PixelTextField(
                value = phoneText,
                onValueChange = { phoneText = it },
                label = "Phone number",
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone),
            )
            PixelTextField(
                value = relayText,
                onValueChange = { relayText = it },
                label = "Relay (empty = coordinator host :${RelayRules.DEFAULT_RELAY_PORT}, or off)",
                placeholder = RelayRules.defaultFor(urlText) ?: "10.35.114.52:${RelayRules.DEFAULT_RELAY_PORT}",
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
            inputError?.let { Text(it, color = PixelColors.Bad) }
            PixelButton(
                text = if (checking) "Checking..." else "Save and test connection",
                enabled = !checking,
                onClick = {
                    val url = NetworkRules.normalizeBaseUrl(urlText)
                    val phone = NetworkRules.normalizePhone(phoneText)
                    when {
                        url == null ->
                            inputError = "Enter the full address, for example http://10.35.114.52:8080"
                        phone == null ->
                            inputError = "Enter a phone number with 10 to 15 digits"
                        RelayRules.problemWith(relayText) != null ->
                            inputError = RelayRules.problemWith(relayText)
                        else -> {
                            inputError = null
                            store.save(url, phone)
                            store.saveRelay(relayText)
                            urlText = url
                            phoneText = phone
                            status = NetworkStatusReader.read(context)
                            checking = true
                            scope.launch {
                                result = CoordinatorCheck.check(url)
                                checking = false
                            }
                        }
                    }
                },
            )

            when (val r = result) {
                is CheckResult.Reachable -> {
                    Text("Coordinator reachable (${r.latencyMs} ms)", style = MaterialTheme.typography.titleSmall, color = PixelColors.Good)
                    val ahead = if (r.skewSeconds >= 0) "ahead of" else "behind"
                    Text("Phone clock is ${kotlin.math.abs(r.skewSeconds)} s $ahead the coordinator.")
                    if (NetworkRules.isClockSkewTooLarge(r.skewSeconds)) {
                        Text("That is too large. Turn on automatic date and time in Android settings.", color = PixelColors.Bad)
                    }
                }
                is CheckResult.Failed -> Text(r.reason, color = PixelColors.Bad)
                null -> Unit
            }
        }
    }
}

/**
 * Which address the network will use to reach this phone. Audits and uploads connect TO the phone, and on the
 * Android 14 and 16 test phones incoming connections over a VPN were never delivered to the app (they were over
 * USB tethering), so ZeroTier only works for heartbeats there. A Wi-Fi shared with the coordinator works.
 */
@Composable
private fun AdvertisedAddressPanel(status: NetworkStatus, coordinatorUrl: String, relay: String?) {
    val host = try {
        java.net.URI(coordinatorUrl).host
    } catch (e: java.net.URISyntaxException) {
        null
    }
    val picked = AddressPicker.pick(status.allAddresses, host)
    PixelPanel(title = "This phone's address") {
        if (picked == null) {
            Text("No network address yet. Connect to the coordinator's Wi-Fi or turn on ZeroTier.", color = PixelColors.Bad)
            return@PixelPanel
        }
        Text("Advertised to the network: ${picked.address} " + if (picked.isVpn) "(ZeroTier)" else "(Wi-Fi / local network)")
        if (picked.isVpn && relay != null) {
            Text(
                "Android does not deliver incoming ZeroTier connections to apps, so audits and file uploads reach " +
                    "this phone through the relay at $relay. Run `go run ./cmd/relay` on that machine.",
                color = PixelColors.Good,
            )
        } else if (picked.isVpn) {
            Text(
                "Warning: the relay is off. Android does not deliver incoming ZeroTier connections to apps, so " +
                    "audits and file uploads cannot reach this phone. Set a relay below (or leave it empty for the default).",
                color = PixelColors.Bad,
            )
        } else {
            Text("Same network as the coordinator: audits and uploads can reach this phone.", color = PixelColors.Good)
        }
    }
}
