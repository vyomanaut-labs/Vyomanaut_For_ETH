package org.vyomanaut.provider.ui.pixel

import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

private const val TITLE_SHADOW_OFFSET = 6f
private const val PRESS_SHIFT_DP = 3
private const val BAR_SEGMENTS = 10
private const val HINT_ALPHA = 0.5f
private val titleShadowColor = Color(0xFF3A1C7A)

/** Big screen heading with a hard pixel shadow. */
@Composable
fun ScreenTitle(title: String, subtitle: String, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(
            title,
            style = MaterialTheme.typography.headlineLarge.copy(
                shadow = Shadow(titleShadowColor, Offset(TITLE_SHADOW_OFFSET, TITLE_SHADOW_OFFSET), 0f),
            ),
            color = PixelColors.Pink,
            textAlign = TextAlign.Center,
        )
        Text(subtitle, style = MaterialTheme.typography.titleSmall, color = PixelColors.Muted, textAlign = TextAlign.Center)
    }
}

/** A bevelled pixel card. */
@Composable
fun PixelPanel(
    modifier: Modifier = Modifier,
    title: String? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        modifier
            .fillMaxWidth()
            .pixelFrame(PixelColors.Panel, PixelColors.PanelLight, PixelColors.PanelDark)
            .padding(18.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (title != null) {
            Text(title, style = MaterialTheme.typography.titleMedium, color = PixelColors.Pink)
        }
        content()
    }
}

/** A chunky block button that sinks a little when pressed. */
@Composable
fun PixelButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    color: Color = PixelColors.Accent,
) {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val fill = if (enabled) color else PixelColors.Disabled
    Box(
        modifier
            .fillMaxWidth()
            .offset(y = if (pressed) PRESS_SHIFT_DP.dp else 0.dp)
            .pixelFrame(fill, pixelLight(fill), pixelDark(fill))
            .clickable(interactionSource = interaction, indication = null, enabled = enabled, onClick = onClick)
            .padding(vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = MaterialTheme.typography.titleMedium,
            color = if (enabled) Color.White else PixelColors.Muted,
        )
    }
}

/** A segmented power bar. [fraction] is 0..1. */
@Composable
fun PixelBar(fraction: Float, modifier: Modifier = Modifier, segments: Int = BAR_SEGMENTS) {
    val filled = (fraction.coerceIn(0f, 1f) * segments).roundToInt()
    Row(modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        repeat(segments) { index ->
            val on = index < filled
            val fill = if (on) PixelColors.Pink else PixelColors.Field
            Box(
                Modifier
                    .weight(1f)
                    .height(26.dp)
                    .pixelFrame(
                        fill,
                        if (on) pixelLight(fill) else fill,
                        if (on) pixelDark(fill) else fill,
                        unit = 3.dp,
                    ),
            )
        }
    }
}

/** A single-line text field with a recessed pixel frame. */
@Composable
fun PixelTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    placeholder: String? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MaterialTheme.typography.labelLarge, color = PixelColors.Muted)
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyLarge.copy(color = PixelColors.Text),
            cursorBrush = SolidColor(PixelColors.Pink),
            keyboardOptions = keyboardOptions,
            modifier = Modifier
                .fillMaxWidth()
                .pixelFrame(PixelColors.Field, PixelColors.PanelDark, PixelColors.PanelLight, unit = 3.dp)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            decorationBox = { inner ->
                Box {
                    if (value.isEmpty() && placeholder != null) {
                        Text(
                            placeholder,
                            style = MaterialTheme.typography.bodyLarge,
                            color = PixelColors.Muted.copy(alpha = HINT_ALPHA),
                        )
                    }
                    inner()
                }
            },
        )
    }
}

/** Bottom tab bar made of pixel blocks. */
@Composable
fun PixelNavBar(labels: List<String>, selectedIndex: Int, onSelect: (Int) -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .drawBehind { drawRect(PixelColors.PanelDark) }
            .navigationBarsPadding()
            .padding(10.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        labels.forEachIndexed { index, label ->
            val selected = index == selectedIndex
            val fill = if (selected) PixelColors.Accent else PixelColors.Panel
            Box(
                Modifier
                    .weight(1f)
                    .pixelFrame(fill, pixelLight(fill), pixelDark(fill), unit = 3.dp)
                    .clickable { onSelect(index) }
                    .padding(vertical = 12.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    label,
                    style = MaterialTheme.typography.titleSmall,
                    color = if (selected) Color.White else PixelColors.Muted,
                )
            }
        }
    }
}
