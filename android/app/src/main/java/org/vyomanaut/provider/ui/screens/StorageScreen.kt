package org.vyomanaut.provider.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import org.vyomanaut.provider.config.DemoProfile
import org.vyomanaut.provider.storage.BYTES_PER_MIB
import org.vyomanaut.provider.storage.LimitRange
import org.vyomanaut.provider.storage.LimitStore
import org.vyomanaut.provider.storage.StatFsFreeSpace
import org.vyomanaut.provider.storage.StorageLimitPolicy
import org.vyomanaut.provider.storage.formatGiB
import org.vyomanaut.provider.ui.pixel.PixelBar
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel
import org.vyomanaut.provider.ui.pixel.ScreenTitle

@Composable
fun StorageScreen() {
    val context = LocalContext.current
    val store = remember { LimitStore(context) }
    val source = remember { StatFsFreeSpace(context.filesDir) }

    var freeBytes by remember { mutableLongStateOf(source.freeBytes()) }
    val totalBytes = remember { source.totalBytes() }
    var savedBytes by remember { mutableLongStateOf(store.limitBytes()) }

    val range = StorageLimitPolicy.range(freeBytes)

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ScreenTitle("Storage limit", "Set your power bar")

        PixelPanel(title = "Your phone") {
            Text("Free space: ${formatGiB(freeBytes)} of ${formatGiB(totalBytes)}")
            Text("You can offer at most ${DemoProfile.MAX_LIMIT_PERCENT_OF_FREE}% of your free space.", color = PixelColors.Muted)
            PixelButton(text = "Refresh", color = PixelColors.PanelLight, onClick = { freeBytes = source.freeBytes() })
        }

        when (range) {
            is LimitRange.NotEnoughSpace -> PixelPanel(title = "Not enough free space") {
                Text(
                    "The smallest limit is ${formatGiB(DemoProfile.MIN_LIMIT_BYTES)}, which needs about " +
                        "${formatGiB(range.requiredFreeBytes)} free. Free up space and tap Refresh.",
                )
            }

            is LimitRange.Available -> LimitPicker(
                range = range,
                savedBytes = savedBytes,
                onSave = { bytes ->
                    store.save(bytes)
                    savedBytes = bytes
                },
            )
        }

        if (savedBytes > 0L) {
            PixelPanel(title = "Saved limit") {
                Text(formatGiB(savedBytes), style = MaterialTheme.typography.headlineSmall, color = PixelColors.Good)
                if (range is LimitRange.Available && savedBytes > range.maxBytes) {
                    Text("Your saved limit is above the current cap. Pick a lower one and save again.", color = PixelColors.Bad)
                }
            }
        }
    }
}

@Composable
private fun LimitPicker(
    range: LimitRange.Available,
    savedBytes: Long,
    onSave: (Long) -> Unit,
) {
    val minMiB = range.minBytes / BYTES_PER_MIB
    val maxMiB = range.maxBytes / BYTES_PER_MIB
    val startBytes = if (savedBytes > 0L) StorageLimitPolicy.clamp(savedBytes, range) else range.maxBytes
    var sliderMiB by remember(range) { mutableFloatStateOf((startBytes / BYTES_PER_MIB).toFloat()) }

    PixelPanel(title = "You offer") {
        val chosenBytes = StorageLimitPolicy.clamp(sliderMiB.toLong() * BYTES_PER_MIB, range)
        Text(formatGiB(chosenBytes), style = MaterialTheme.typography.displaySmall, color = PixelColors.Gold)
        PixelBar(fraction = chosenBytes.toFloat() / range.maxBytes.toFloat())
        if (maxMiB > minMiB) {
            Slider(
                value = sliderMiB,
                onValueChange = { sliderMiB = it },
                valueRange = minMiB.toFloat()..maxMiB.toFloat(),
                colors = SliderDefaults.colors(
                    thumbColor = PixelColors.Pink,
                    activeTrackColor = PixelColors.Accent,
                    inactiveTrackColor = PixelColors.Field,
                ),
            )
            Text("From ${formatGiB(range.minBytes)} to ${formatGiB(range.maxBytes)}", color = PixelColors.Muted)
        } else {
            Text("Your free space allows exactly this limit.", color = PixelColors.Muted)
        }
        PixelButton(text = "Save limit", color = PixelColors.Accent, onClick = { onSave(chosenBytes) })
    }
}
