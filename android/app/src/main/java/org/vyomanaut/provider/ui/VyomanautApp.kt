package org.vyomanaut.provider.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Scaffold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import org.vyomanaut.provider.network.JoinLink
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelNavBar
import org.vyomanaut.provider.ui.pixel.SpaceBackground
import org.vyomanaut.provider.ui.screens.HomeScreen
import org.vyomanaut.provider.ui.screens.JoinDialog
import org.vyomanaut.provider.ui.screens.NetworkScreen
import org.vyomanaut.provider.ui.screens.PaymentsScreen
import org.vyomanaut.provider.ui.screens.StorageScreen

enum class Tab(val label: String) {
    Home("Home"),
    Payments("Payments"),
    Storage("Storage"),
    Network("Network"),
}

@Composable
fun VyomanautApp(pendingJoin: JoinLink? = null, onJoinHandled: () -> Unit = {}) {
    var tab by rememberSaveable { mutableStateOf(Tab.Home) }
    // Bumped after a join so every screen re-reads the saved settings.
    var joinCount by rememberSaveable { mutableStateOf(0) }
    if (pendingJoin != null) {
        JoinDialog(
            link = pendingJoin,
            onJoined = {
                joinCount++
                tab = Tab.Home
                onJoinHandled()
            },
            onDismiss = onJoinHandled,
        )
    }
    Box(modifier = Modifier.fillMaxSize()) {
        SpaceBackground()
        Scaffold(
            modifier = Modifier.fillMaxSize(),
            containerColor = Color.Transparent,
            contentColor = PixelColors.Text,
            bottomBar = {
                PixelNavBar(
                    labels = Tab.entries.map { it.label },
                    selectedIndex = tab.ordinal,
                    onSelect = { tab = Tab.entries[it] },
                )
            },
        ) { padding ->
            androidx.compose.runtime.key(joinCount) {
            Box(modifier = Modifier.padding(padding).fillMaxSize()) {
                when (tab) {
                    Tab.Home -> HomeScreen(onOpenNetwork = { tab = Tab.Network }, onOpenStorage = { tab = Tab.Storage })
                    Tab.Payments -> PaymentsScreen()
                    Tab.Storage -> StorageScreen()
                    Tab.Network -> NetworkScreen()
                }
            }
            }
        }
    }
}
