package org.vyomanaut.provider.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import org.vyomanaut.provider.R

/** Bebas Neue (SIL Open Font License), bundled in res/font. It has one weight and draws lowercase as capitals. */
val BebasNeue = FontFamily(Font(R.font.bebas_neue_regular, FontWeight.Normal))

private const val LINE_HEIGHT_RATIO = 1.25f
private const val LETTER_SPACING_SP = 0.8f

// Bebas Neue is a narrow, short font, so every style is set larger than the Material default.
private fun TextStyle.bebas(sizeSp: Int): TextStyle = copy(
    fontFamily = BebasNeue,
    fontSize = sizeSp.sp,
    lineHeight = (sizeSp * LINE_HEIGHT_RATIO).sp,
    letterSpacing = LETTER_SPACING_SP.sp,
)

private val base = Typography()

val Typography = Typography(
    displayLarge = base.displayLarge.bebas(56),
    displayMedium = base.displayMedium.bebas(48),
    displaySmall = base.displaySmall.bebas(40),
    headlineLarge = base.headlineLarge.bebas(42),
    headlineMedium = base.headlineMedium.bebas(34),
    headlineSmall = base.headlineSmall.bebas(28),
    titleLarge = base.titleLarge.bebas(26),
    titleMedium = base.titleMedium.bebas(22),
    titleSmall = base.titleSmall.bebas(19),
    bodyLarge = base.bodyLarge.bebas(20),
    bodyMedium = base.bodyMedium.bebas(18),
    bodySmall = base.bodySmall.bebas(16),
    labelLarge = base.labelLarge.bebas(20),
    labelMedium = base.labelMedium.bebas(18),
    labelSmall = base.labelSmall.bebas(16)
)
