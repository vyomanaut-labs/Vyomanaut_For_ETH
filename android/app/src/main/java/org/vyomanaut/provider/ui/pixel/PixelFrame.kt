package org.vyomanaut.provider.ui.pixel

import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

private const val LIGHT_MIX = 0.3f
private const val DARK_MIX = 0.35f

fun pixelLight(color: Color): Color = lerp(color, Color.White, LIGHT_MIX)

fun pixelDark(color: Color): Color = lerp(color, Color.Black, DARK_MIX)

/**
 * Draws a chunky pixel box behind the content: a dark outline with notched corners, a flat fill,
 * a light bevel on the top and left edges and a dark bevel on the bottom and right edges.
 */
fun Modifier.pixelFrame(
    fill: Color,
    light: Color,
    dark: Color,
    border: Color = PixelColors.Border,
    unit: Dp = 4.dp,
): Modifier = drawBehind {
    val u = unit.toPx().roundToInt().coerceAtLeast(1).toFloat()
    val w = size.width
    val h = size.height
    if (w < 6f * u || h < 6f * u) {
        drawRect(fill)
        return@drawBehind
    }
    drawNotched(border, 0f, u)
    drawNotched(fill, u, u)
    drawRect(light, Offset(2f * u, u), Size(w - 4f * u, u))
    drawRect(light, Offset(u, 2f * u), Size(u, h - 4f * u))
    drawRect(dark, Offset(2f * u, h - 2f * u), Size(w - 4f * u, u))
    drawRect(dark, Offset(w - 2f * u, 2f * u), Size(u, h - 4f * u))
}

private fun DrawScope.drawNotched(color: Color, inset: Float, u: Float) {
    val w = size.width - 2f * inset
    val h = size.height - 2f * inset
    drawRect(color, Offset(inset + u, inset), Size(w - 2f * u, h))
    drawRect(color, Offset(inset, inset + u), Size(w, h - 2f * u))
}
