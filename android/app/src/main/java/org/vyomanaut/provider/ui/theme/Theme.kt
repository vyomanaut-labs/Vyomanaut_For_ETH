package org.vyomanaut.provider.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import org.vyomanaut.provider.ui.pixel.PixelColors

// One fixed dark purple scheme: the pixel look does not follow the phone's light/dark or wallpaper colours.
private val PixelColorScheme = darkColorScheme(
    primary = PixelColors.Accent,
    onPrimary = Color.White,
    secondary = PixelColors.Pink,
    onSecondary = PixelColors.Border,
    tertiary = PixelColors.Gold,
    onTertiary = PixelColors.Border,
    background = PixelColors.Space,
    onBackground = PixelColors.Text,
    surface = PixelColors.Panel,
    onSurface = PixelColors.Text,
    surfaceVariant = PixelColors.PanelDark,
    onSurfaceVariant = PixelColors.Muted,
    outline = PixelColors.Accent,
    error = PixelColors.Bad,
    onError = PixelColors.Border,
)

@Composable
fun Vyomanaut_AppTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = PixelColorScheme,
        typography = Typography,
        content = content,
    )
}
