package org.vyomanaut.provider.service

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Process-wide view of whether the provider service is running. The UI observes this. */
object ProviderRuntime {
    private val mutableRunning = MutableStateFlow(false)
    val running: StateFlow<Boolean> = mutableRunning.asStateFlow()

    internal fun setRunning(value: Boolean) {
        mutableRunning.value = value
    }
}
