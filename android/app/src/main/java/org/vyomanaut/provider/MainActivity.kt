package org.vyomanaut.provider

import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import org.vyomanaut.provider.engine.ProviderEngine
import org.vyomanaut.provider.ui.VyomanautApp
import org.vyomanaut.provider.ui.theme.Vyomanaut_AppTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        ProviderEngine.restoreLog(this)
        // The app is always dark, so the status and navigation bar icons must always be light.
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
        )
        setContent {
            Vyomanaut_AppTheme {
                VyomanautApp()
            }
        }
    }
}
