package org.vyomanaut.provider.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import org.vyomanaut.provider.engine.EngineStore
import org.vyomanaut.provider.engine.ProviderEngine
import org.vyomanaut.provider.engine.isActive
import org.vyomanaut.provider.network.JoinLink
import org.vyomanaut.provider.network.JoinNetwork
import org.vyomanaut.provider.network.NetworkRules
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.service.ProviderController
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel
import org.vyomanaut.provider.ui.pixel.PixelTextField

/**
 * Shown when the app is opened from a network's join page. One tap saves the coordinator, relay and chain API;
 * the person only types their phone number.
 */
@Composable
fun JoinDialog(link: JoinLink, onJoined: () -> Unit, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val store = remember { OnboardingStore(context) }
    var phone by remember { mutableStateOf(store.phone()) }
    var error by remember { mutableStateOf<String?>(null) }
    val engineState by ProviderEngine.state.collectAsState()
    val current = store.coordinatorUrl()
    val switching = current.isNotEmpty() && current != link.coordinatorUrl
    val alreadyHere = !switching && EngineStore(context).isRegisteredFor(link.coordinatorUrl)

    Dialog(onDismissRequest = onDismiss) {
        PixelPanel(title = "Join ${link.networkName}") {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                Text(
                    "Lend some free space to this network. Your phone keeps encrypted pieces of other people's files " +
                        "and cannot read them.",
                )
                Text("Coordinator: ${link.coordinatorUrl}", color = PixelColors.Muted, style = MaterialTheme.typography.bodySmall)
                link.relay?.let { Text("Relay: $it", color = PixelColors.Muted, style = MaterialTheme.typography.bodySmall) }
                if (switching) {
                    Text(
                        "This phone was set up for another network ($current). Joining this one " +
                            (if (engineState.isActive) "stops the provider and " else "") + "asks you to register again.",
                        color = PixelColors.Gold,
                    )
                }
                if (alreadyHere) {
                    Text("This phone is already registered here.", color = PixelColors.Good)
                }
                PixelTextField(
                    value = phone,
                    onValueChange = { phone = it },
                    label = "Your phone number, with country code",
                    placeholder = "+919876500001",
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone),
                )
                error?.let { Text(it, color = PixelColors.Bad) }
                PixelButton(text = "Join", onClick = {
                    val normalized = NetworkRules.normalizePhone(phone)
                    if (normalized == null) {
                        error = "Enter a phone number with 10 to 15 digits, for example +919876500001."
                    } else {
                        if (switching && engineState.isActive) ProviderController.stop(context)
                        JoinNetwork.apply(context, link, normalized)
                        onJoined()
                    }
                })
                PixelButton(text = "Not now", color = PixelColors.PanelLight, onClick = onDismiss)
            }
        }
    }
}
