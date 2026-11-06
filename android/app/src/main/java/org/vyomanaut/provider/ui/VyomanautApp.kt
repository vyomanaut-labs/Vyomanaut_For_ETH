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
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelNavBar
import org.vyomanaut.provider.ui.pixel.SpaceBackground
import org.vyomanaut.provider.ui.screens.HomeScreen
import org.vyomanaut.provider.ui.screens.NetworkScreen
import org.vyomanaut.provider.ui.screens.StorageScreen

enum class Tab(val label: String) {
    Home("Home"),
    Storage("Storage"),
    Network("Network"),
}

@Composable
fun VyomanautApp() {
    var tab by rememberSaveable { mutableStateOf(Tab.Home) }
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
            Box(modifier = Modifier.padding(padding).fillMaxSize()) {
                when (tab) {
                    Tab.Home -> HomeScreen()
                    Tab.Storage -> StorageScreen()
                    Tab.Network -> NetworkScreen()
                }
            }
        }
    }
}
