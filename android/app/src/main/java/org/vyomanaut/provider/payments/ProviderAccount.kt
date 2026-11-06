package org.vyomanaut.provider.payments

import org.json.JSONException
import org.json.JSONObject

/** What `provider onboard` saved in registration.json: enough to call the coordinator as this provider. */
data class Registration(val providerId: String, val token: String)

/** The provider's own view from GET /api/v1/provider/{id}/status. */
data class ProviderStatus(
    val status: String,
    /** The ledger's running balance for this provider (not yet paid out). */
    val pendingEarningsPaise: Long,
    val heldEarningsPaise: Long,
    val consecutiveAuditPasses: Int,
    /** 0.0-1.0, or null before the first scored audit. */
    val scoreComposite: Double?,
    val lastHeartbeat: String?,
    val storedChunks: Int,
    val networkMode: String,
)

data class AuditReceipt(val challengedAt: String, val result: String?, val latencyMs: Int?)

/** GET /api/v1/provider/receipts, newest first as the coordinator returns them. */
data class ReceiptPage(val total: Int, val receipts: List<AuditReceipt>) {
    val passed: Int get() = receipts.count { it.result == RESULT_PASS }
    val failed: Int get() = receipts.count { it.result != null && it.result != RESULT_PASS }

    companion object {
        const val RESULT_PASS = "PASS"
    }
}

/** Pure JSON parsing for the coordinator's provider endpoints (shapes from internal/api/provider.go). */
object ProviderAccountParser {

    fun registration(json: String): Registration? = try {
        val o = JSONObject(json)
        val id = o.optString("provider_id")
        val token = o.optString("token")
        if (id.isBlank() || token.isBlank()) null else Registration(id, token)
    } catch (e: JSONException) {
        null
    }

    @Throws(JSONException::class)
    fun status(json: String): ProviderStatus {
        val o = JSONObject(json)
        return ProviderStatus(
            status = o.getString("status"),
            pendingEarningsPaise = o.optLong("pending_earnings_paise", 0),
            heldEarningsPaise = o.optLong("held_earnings_paise", 0),
            consecutiveAuditPasses = o.optInt("consecutive_audit_passes", 0),
            scoreComposite = if (o.isNull("score_composite")) null else o.getDouble("score_composite"),
            lastHeartbeat = if (o.isNull("last_heartbeat_ts")) null else o.getString("last_heartbeat_ts"),
            storedChunks = o.optInt("stored_chunks", 0),
            networkMode = o.optString("network_mode", ""),
        )
    }

    @Throws(JSONException::class)
    fun receipts(json: String): ReceiptPage {
        val o = JSONObject(json)
        val list = o.optJSONArray("receipts")
        val receipts = (0 until (list?.length() ?: 0)).map { i ->
            val r = list!!.getJSONObject(i)
            AuditReceipt(
                challengedAt = r.optString("server_challenge_ts"),
                result = if (r.isNull("audit_result")) null else r.getString("audit_result"),
                latencyMs = if (r.isNull("response_latency_ms")) null else r.getInt("response_latency_ms"),
            )
        }
        return ReceiptPage(total = o.optInt("total", receipts.size), receipts = receipts)
    }

    /** The coordinator's `error_code` from an error body, or null. */
    fun errorCode(json: String): String? = try {
        JSONObject(json).optString("error_code").ifBlank { null }
    } catch (e: JSONException) {
        null
    }
}
