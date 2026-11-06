package org.vyomanaut.provider

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.mutableStateOf
import org.vyomanaut.provider.engine.ProviderEngine
import org.vyomanaut.provider.network.JoinLink
import org.vyomanaut.provider.network.JoinLinks
import org.vyomanaut.provider.ui.VyomanautApp
import org.vyomanaut.provider.ui.theme.Vyomanaut_AppTheme

class MainActivity : ComponentActivity() {
    /** A network to join, from a `vyomanaut://join?...` link (the coordinator's join page). */
    private val pendingJoin = mutableStateOf<JoinLink?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        ProviderEngine.restoreLog(this)
        if (savedInstanceState == null) readJoinLink(intent)
        // The app is always dark, so the status and navigation bar icons must always be light.
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
        )
        setContent {
            Vyomanaut_AppTheme {
                VyomanautApp(pendingJoin = pendingJoin.value, onJoinHandled = { pendingJoin.value = null })
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        readJoinLink(intent)
    }

    private fun readJoinLink(intent: Intent?) {
        val data = intent?.takeIf { it.action == Intent.ACTION_VIEW }?.dataString ?: return
        JoinLinks.parse(data)?.let { pendingJoin.value = it }
    }
}
