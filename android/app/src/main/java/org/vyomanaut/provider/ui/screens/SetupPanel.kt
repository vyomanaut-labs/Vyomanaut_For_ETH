package org.vyomanaut.provider.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import org.vyomanaut.provider.power.SetupStep
import org.vyomanaut.provider.power.SetupSteps
import org.vyomanaut.provider.power.StepAction
import org.vyomanaut.provider.power.StepState
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel

/**
 * The "Get connected" checklist at the top of Home: every requirement in order, the next one opened up with its
 * button. Once the phone is online it folds to one line.
 */
@Composable
fun SetupPanel(
    steps: List<SetupStep>,
    onAction: (StepAction) -> Unit,
    onConfirm: (StepAction) -> Unit,
    message: String?,
) {
    val next = SetupSteps.next(steps)
    val (done, total) = SetupSteps.progress(steps)
    val allDone = done == total

    PixelPanel(title = if (allDone) "Connected" else "Get connected: $done of $total done") {
        if (allDone) {
            Text(steps.last().detail, color = PixelColors.Good)
            return@PixelPanel
        }
        steps.forEach { step ->
            val isNext = step == next
            Row(modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
                Text(
                    marker(step.state),
                    fontFamily = FontFamily.Monospace,
                    color = colorOf(step.state, isNext),
                    modifier = Modifier.width(36.dp),
                )
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(step.title, color = colorOf(step.state, isNext))
                    if (isNext || step.state == StepState.OPTIONAL) {
                        Text(step.detail, style = MaterialTheme.typography.bodySmall, color = PixelColors.Text)
                    }
                    if ((isNext || step.state == StepState.OPTIONAL) && step.action != StepAction.NONE) {
                        PixelButton(
                            text = step.actionLabel,
                            color = if (isNext) PixelColors.Accent else PixelColors.PanelLight,
                            onClick = { onAction(step.action) },
                        )
                        // Steps done in another app: the phone cannot tell, so the user says so.
                        if (step.action == StepAction.ZEROTIER || step.action == StepAction.OEM) {
                            PixelButton(text = "I did this", color = PixelColors.PanelLight, onClick = { onConfirm(step.action) })
                        }
                    }
                }
            }
        }
        message?.let { Text(it, color = PixelColors.Bad) }
    }
}

private fun marker(state: StepState): String = when (state) {
    StepState.DONE -> "[ok]"
    StepState.TODO -> "[  ]"
    StepState.WAITING -> "[..]"
    StepState.OPTIONAL -> "[ +]"
    StepState.BLOCKED -> "[  ]"
}

private fun colorOf(state: StepState, isNext: Boolean) = when {
    state == StepState.DONE -> PixelColors.Good
    isNext -> PixelColors.Gold
    state == StepState.BLOCKED -> PixelColors.Muted
    else -> PixelColors.Text
}
