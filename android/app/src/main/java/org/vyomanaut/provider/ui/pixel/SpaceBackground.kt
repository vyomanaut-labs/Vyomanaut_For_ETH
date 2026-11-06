package org.vyomanaut.provider.ui.pixel

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

private const val STAR_COUNT = 90
private const val STAR_SEED = 12345L
private const val LCG_MULTIPLIER = 1103515245L
private const val LCG_INCREMENT = 12345L
private const val LCG_MASK = 0x7fffffffL
private const val LCG_RESOLUTION = 10000L
private const val BIG_STAR_CHANCE = 0.9f
private const val WHITE_STAR_CHANCE = 0.5f
private const val STAR_ALPHA = 0.85f
private const val RING_WIDTH_PER_RADIUS = 17
private const val RING_HEIGHT_PER_RADIUS = 5
private const val RING_DIVISOR = 10
private const val RING_INNER = 0.55f
private const val BAND_EVERY = 4

private class Planet(
    val centerX: Float,
    val centerY: Float,
    val radiusCells: Int,
    val cell: Dp,
    val tones: List<Color>,
    val ring: Color? = null,
    val bands: Boolean = false,
)

private val planets = listOf(
    // Ringed planet, top right.
    Planet(0.86f, 0.09f, 6, 7.dp, listOf(Color(0xFFFFD1F0), Color(0xFFFF8AD8), Color(0xFFB35CD6), Color(0xFF6A2C9E)), ring = Color(0xFFFFE08A)),
    // Small moon, left.
    Planet(0.07f, 0.40f, 3, 6.dp, listOf(Color(0xFFE6E0FF), Color(0xFFB5A8E8), Color(0xFF8577C4), Color(0xFF5A4E96))),
    // Banded gas giant, half off the right edge.
    Planet(0.98f, 0.70f, 8, 8.dp, listOf(Color(0xFF9B5CFF), Color(0xFF7A3FD6), Color(0xFF5A2AA8), Color(0xFF3A1B78)), bands = true),
    // Tiny planet, bottom left.
    Planet(0.15f, 0.90f, 2, 5.dp, listOf(Color(0xFFFFD86B), Color(0xFFE8A04A), Color(0xFFB36A2E), Color(0xFF7A4020))),
)

/** Full-screen purple space with a pixel starfield and pixel planets. Drawn once, no animation (saves battery). */
@Composable
fun SpaceBackground(modifier: Modifier = Modifier) {
    Canvas(modifier.fillMaxSize()) {
        drawRect(Brush.verticalGradient(listOf(PixelColors.Space, PixelColors.SpaceMid, PixelColors.Space)))
        drawStars()
        planets.forEach { drawPlanet(it) }
    }
}

private fun DrawScope.drawStars() {
    val cell = 3.dp.toPx().roundToInt().coerceAtLeast(1).toFloat()
    var seed = STAR_SEED
    fun next(): Float {
        seed = (seed * LCG_MULTIPLIER + LCG_INCREMENT) and LCG_MASK
        return (seed % LCG_RESOLUTION).toFloat() / LCG_RESOLUTION
    }
    repeat(STAR_COUNT) {
        val x = (next() * size.width / cell).toInt() * cell
        val y = (next() * size.height / cell).toInt() * cell
        val big = next() > BIG_STAR_CHANCE
        val color = (if (next() > WHITE_STAR_CHANCE) Color.White else PixelColors.Muted).copy(alpha = STAR_ALPHA)
        val unitSize = Size(cell, cell)
        drawRect(color, Offset(x, y), unitSize)
        if (big) {
            drawRect(color, Offset(x - cell, y), unitSize)
            drawRect(color, Offset(x + cell, y), unitSize)
            drawRect(color, Offset(x, y - cell), unitSize)
            drawRect(color, Offset(x, y + cell), unitSize)
        }
    }
}

private fun DrawScope.drawPlanet(planet: Planet) {
    val cell = planet.cell.toPx().roundToInt().coerceAtLeast(1).toFloat()
    val r = planet.radiusCells
    val originX = (planet.centerX * size.width / cell).roundToInt() * cell
    val originY = (planet.centerY * size.height / cell).roundToInt() * cell
    val cellSize = Size(cell, cell)
    val last = planet.tones.size - 1

    fun cellAt(ix: Int, iy: Int, color: Color) =
        drawRect(color, Offset(originX + ix * cell, originY + iy * cell), cellSize)

    val ringColor = planet.ring
    val ringX = r * RING_WIDTH_PER_RADIUS / RING_DIVISOR
    val ringY = (r * RING_HEIGHT_PER_RADIUS / RING_DIVISOR).coerceAtLeast(1)

    fun drawRingHalf(front: Boolean) {
        if (ringColor == null) return
        for (iy in -ringY..ringY) {
            for (ix in -ringX..ringX) {
                if ((iy >= 1) != front) continue
                val v = (ix * ix).toFloat() / (ringX * ringX) + (iy * iy).toFloat() / (ringY * ringY)
                if (v in RING_INNER..1f) cellAt(ix, iy, ringColor)
            }
        }
    }

    drawRingHalf(front = false)
    for (iy in -r..r) {
        for (ix in -r..r) {
            if (ix * ix + iy * iy > r * r) continue
            val light = ((ix + iy).toFloat() / (2f * r) + 1f) / 2f
            var index = (light * planet.tones.size).toInt().coerceIn(0, last)
            if (planet.bands && (iy + r) % BAND_EVERY == 0) index = (index + 1).coerceAtMost(last)
            cellAt(ix, iy, planet.tones[index])
        }
    }
    drawRingHalf(front = true)
}
