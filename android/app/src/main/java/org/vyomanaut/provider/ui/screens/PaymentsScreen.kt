package org.vyomanaut.provider.ui.screens

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import org.vyomanaut.provider.network.OnboardingStore
import org.vyomanaut.provider.payments.CoordinatorAccountApi
import org.vyomanaut.provider.payments.CoordinatorWallet
import org.vyomanaut.provider.payments.Fetch
import org.vyomanaut.provider.payments.Money
import org.vyomanaut.provider.payments.PaymentsSettings
import org.vyomanaut.provider.payments.ProviderStatus
import org.vyomanaut.provider.payments.ReceiptPage
import org.vyomanaut.provider.payments.WalletSnapshot
import org.vyomanaut.provider.ui.pixel.PixelButton
import org.vyomanaut.provider.ui.pixel.PixelColors
import org.vyomanaut.provider.ui.pixel.PixelPanel
import org.vyomanaut.provider.ui.pixel.PixelTextField
import org.vyomanaut.provider.ui.pixel.ScreenTitle
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException

private const val REFRESH_EVERY_MS = 60_000L
private const val RECEIPTS_SHOWN = 6
private const val PERCENT = 100
private const val SECONDS_PER_MINUTE = 60

private val timeFormat: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM HH:mm:ss")

/**
 * Payments tab: what this phone has earned (from the coordinator's ledger), how payouts work, and the
 * coordinator wallet that decides and pays them (read from the chain, so anyone can check the numbers).
 */
@Composable
fun PaymentsScreen() {
    val context = LocalContext.current
    val settings = remember { PaymentsSettings(context) }
    val coordinatorUrl = remember { OnboardingStore(context).coordinatorUrl() }

    var refreshTick by remember { mutableIntStateOf(0) }
    var loading by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf<Fetch<ProviderStatus>?>(null) }
    var receipts by remember { mutableStateOf<Fetch<ReceiptPage>?>(null) }
    var wallet by remember { mutableStateOf<Fetch<WalletSnapshot>?>(null) }

    // Refreshes on open, on "Refresh", and every minute while this tab is visible.
    LaunchedEffect(refreshTick) {
        while (true) {
            loading = true
            val registration = CoordinatorAccountApi.registration(context)
            if (coordinatorUrl.isBlank() || registration == null) {
                status = Fetch.Error(CoordinatorAccountApi.MSG_NOT_REGISTERED)
                receipts = null
            } else {
                status = CoordinatorAccountApi.status(coordinatorUrl, registration)
                receipts = CoordinatorAccountApi.receipts(coordinatorUrl, registration)
            }
            val chainApi = settings.chainApiUrl(coordinatorUrl)
            wallet = if (chainApi == null) {
                Fetch.Error("Set the coordinator address on the Network tab first.")
            } else {
                CoordinatorWallet.load(chainApi, settings.rpcUrl())
            }
            loading = false
            delay(REFRESH_EVERY_MS)
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ScreenTitle("Payments", "Earnings and the coordinator wallet")

        PixelButton(
            text = if (loading) "Refreshing..." else "Refresh",
            enabled = !loading,
            color = PixelColors.PanelLight,
            onClick = { refreshTick++ },
        )

        EarningsPanel(status)
        ReceiptsPanel(receipts)
        HowPaymentsWorkPanel((wallet as? Fetch.Ok)?.value)
        WalletPanel(wallet)
        ChainSettingsPanel(settings, coordinatorUrl, onSaved = { refreshTick++ })
    }
}

@Composable
private fun EarningsPanel(status: Fetch<ProviderStatus>?) {
    PixelPanel(title = "My earnings") {
        when (status) {
            null -> Text("Loading...", color = PixelColors.Muted)
            is Fetch.Error -> Text(status.message, color = PixelColors.Bad)
            is Fetch.Ok -> {
                val s = status.value
                Text("Unpaid balance: ${Money.formatPaise(s.pendingEarningsPaise)}", style = MaterialTheme.typography.titleLarge, color = PixelColors.Gold)
                if (s.heldEarningsPaise > 0) Text("Held back: ${Money.formatPaise(s.heldEarningsPaise)}")
                Text(
                    "Provider status: ${s.status}",
                    color = if (s.status == "DEPARTED") PixelColors.Bad else PixelColors.Good,
                )
                Text("Audits passed in a row: ${s.consecutiveAuditPasses}")
                Text("Reliability score: " + (s.scoreComposite?.let { "${(it * PERCENT).toInt()}%" } ?: "not scored yet"))
                Text("Shards stored: ${s.storedChunks}")
                Text("Last heartbeat: " + (s.lastHeartbeat?.let(::localTime) ?: "never"))
                if (s.networkMode == "demo") {
                    Text("Demo network: amounts are test money.", color = PixelColors.Muted)
                }
            }
        }
    }
}

@Composable
private fun ReceiptsPanel(receipts: Fetch<ReceiptPage>?) {
    val page = (receipts as? Fetch.Ok)?.value ?: return
    PixelPanel(title = "Recent audits") {
        if (page.receipts.isEmpty()) {
            Text("No audits yet. The coordinator audits every 2 minutes once this phone stores shards.", color = PixelColors.Muted)
            return@PixelPanel
        }
        Text("Last ${page.receipts.size}: ${page.passed} passed, ${page.failed} failed or timed out")
        SelectionContainer {
            Text(
                page.receipts.take(RECEIPTS_SHOWN).joinToString("\n") { r ->
                    val latency = r.latencyMs?.let { " $it ms" } ?: ""
                    "${localTime(r.challengedAt)}  ${r.result ?: "PENDING"}$latency"
                },
                fontFamily = FontFamily.Monospace,
                color = PixelColors.Text,
            )
        }
    }
}

@Composable
private fun HowPaymentsWorkPanel(wallet: WalletSnapshot?) {
    val window = wallet?.challengeWindowSeconds?.let { formatDuration(it) } ?: "a challenge window"
    PixelPanel(title = "How you get paid") {
        Text("1. Every audit you pass earns money in the coordinator's ledger: your unpaid balance above.")
        Text(
            "2. Each billing period the coordinator wallet posts every provider's share to the escrow contract " +
                "on Whitechain (postPeriod). That wallet decides the payments.",
        )
        Text(
            "3. After $window, during which a guardian can veto a wrong period, the coordinator claims all " +
                "shares in one transaction (claimMany) and pays the gas, so you need no crypto wallet or gas.",
        )
        Text(
            "Payouts go to a wallet the coordinator keeps for this phone. The coordinator does not yet show " +
                "per-provider payout history to the app.",
            color = PixelColors.Muted,
        )
    }
}

@Composable
private fun WalletPanel(wallet: Fetch<WalletSnapshot>?) {
    val context = LocalContext.current
    PixelPanel(title = "Coordinator wallet") {
        when (wallet) {
            null -> Text("Loading...", color = PixelColors.Muted)
            is Fetch.Error -> Text(wallet.message, color = PixelColors.Bad)
            is Fetch.Ok -> {
                val w = wallet.value
                val units = { amount: java.math.BigInteger -> "${Money.formatUnits(amount, w.tokenDecimals)} ${w.tokenSymbol}" }
                SelectionContainer {
                    Text(w.coordinator, fontFamily = FontFamily.Monospace, color = PixelColors.Text)
                }
                Text("Chain ${w.chainId}. Read live from the chain, not from the coordinator.", color = PixelColors.Muted)
                Text("Wallet holds: ${units(w.coordinatorTokenBalance)}")
                Text("Gas: ${Money.formatUnits(w.nativeBalanceWei, CoordinatorWallet.NATIVE_DECIMALS)} WBT")
                w.addressLink(w.coordinator)?.let { link ->
                    PixelButton(text = "Open wallet in explorer", color = PixelColors.PanelLight, onClick = { openUrl(context, link) })
                }

                Text("Escrow ${Money.shortAddress(w.escrow)}", style = MaterialTheme.typography.titleMedium, color = PixelColors.Pink)
                Text("Holds: ${units(w.escrowTokenBalance)}")
                Text("Owners' unspent credit: ${units(w.totalCredit)}")
                Text("Posted to providers, not yet claimed: ${units(w.totalUnclaimed)}")
                w.addressLink(w.escrow)?.let { link ->
                    PixelButton(text = "Open escrow in explorer", color = PixelColors.PanelLight, onClick = { openUrl(context, link) })
                }

                val period = w.lastPeriod
                if (period == null) {
                    Text("No payout period posted yet.", color = PixelColors.Muted)
                } else {
                    Text("Latest period #${period.period}", style = MaterialTheme.typography.titleMedium, color = PixelColors.Pink)
                    Text("Paid out: ${units(period.total)}")
                    Text("Still to claim: ${units(period.unclaimed)}")
                    Text(
                        when {
                            period.vetoed -> "Vetoed by the guardian: this period will not be paid."
                            Instant.now().epochSecond >= period.claimableAt -> "Claimable since ${unixTime(period.claimableAt)}"
                            else -> "Claimable from ${unixTime(period.claimableAt)}"
                        },
                        color = if (period.vetoed) PixelColors.Bad else PixelColors.Text,
                    )
                }
            }
        }
    }
}

@Composable
private fun ChainSettingsPanel(settings: PaymentsSettings, coordinatorUrl: String, onSaved: () -> Unit) {
    var chainApi by remember { mutableStateOf(settings.savedChainApiUrl()) }
    var rpc by remember { mutableStateOf(settings.savedRpcUrl()) }
    PixelPanel(title = "Chain settings") {
        Text("Leave empty to use the defaults.", color = PixelColors.Muted)
        PixelTextField(
            value = chainApi,
            onValueChange = { chainApi = it },
            label = "Coordinator chain API",
            placeholder = settings.chainApiUrl(coordinatorUrl) ?: "http://<coordinator>:8090",
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
        )
        PixelTextField(
            value = rpc,
            onValueChange = { rpc = it },
            label = "Whitechain RPC",
            placeholder = CoordinatorWallet.DEFAULT_RPC_URL,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
        )
        PixelButton(text = "Save", onClick = {
            settings.save(chainApi, rpc)
            onSaved()
        })
    }
}

private fun localTime(iso: String): String = try {
    OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault()).format(timeFormat)
} catch (e: DateTimeParseException) {
    iso
}

private fun unixTime(seconds: Long): String =
    Instant.ofEpochSecond(seconds).atZone(ZoneId.systemDefault()).format(timeFormat)

private fun formatDuration(seconds: Long): String =
    if (seconds % SECONDS_PER_MINUTE == 0L) "${seconds / SECONDS_PER_MINUTE} min" else "$seconds s"

private fun openUrl(context: Context, url: String) {
    try {
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (e: ActivityNotFoundException) {
        // No browser on the phone.
    }
}
