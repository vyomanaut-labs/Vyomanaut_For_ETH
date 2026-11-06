package anchor

import (
	"fmt"
	"io"
	"time"
)

// txHashLen is the length of a transaction hash.
const txHashLen = 32

// shortHex shows the first and last bytes of a hash.
func shortHex(b []byte) string {
	const keep = 6
	if len(b) <= keep*2 {
		return fmt.Sprintf("%x", b)
	}
	return fmt.Sprintf("%x…%x", b[:keep], b[len(b)-keep:])
}

func fullHex(b [32]byte) string { return fmt.Sprintf("0x%x", b[:]) }

// RenderList writes the local record of epochs.
func RenderList(w io.Writer, rows []EpochSummary) {
	if len(rows) == 0 {
		fprintln(w, "no epochs recorded yet")
		return
	}
	fprintf(w, "%-6s %-9s %9s  %-20s  %s\n", "EPOCH", "STATE", "RECEIPTS", "COVERS UP TO", "TRANSACTION")
	for _, r := range rows {
		tx := "-"
		if len(r.AnchorTx) == txHashLen {
			tx = "0x" + shortHex(r.AnchorTx)
		}
		fprintf(w, "%-6d %-9s %9d  %-20s  %s\n", r.Epoch, r.State, r.Leaves,
			r.Last.UTC().Format("2006-01-02 15:04:05"), tx)
		if r.Error != "" {
			fprintf(w, "       last error: %s\n", r.Error)
		}
	}
}

// RenderEpoch writes an epoch report.
func RenderEpoch(w io.Writer, r EpochReport) {
	fprintf(w, "Epoch %d (local state: %s)\n", r.Epoch, r.State)
	fprintf(w, "  database root : %s  (%d receipts)\n", rootOrDash(r.DBRoot, r.DBLeaves > 0), r.DBLeaves)
	if r.Verdict != VerdictNotOnChain {
		fprintf(w, "  chain root    : %s  (%d receipts)\n", fullHex(r.ChainRoot), r.ChainLeaves)
		fprintf(w, "  anchored      : %s", r.ChainTimestamp.Format(time.RFC3339))
		if r.AnchorBlock != 0 {
			fprintf(w, ", block %d", r.AnchorBlock)
		}
		fprintf(w, "\n  finality      : %s\n", describeFinality(r.Finality))
	}
	fprintf(w, "  RESULT        : %s: %s\n", r.Verdict, r.Detail)
}

// RenderReceipt writes a receipt report.
func RenderReceipt(w io.Writer, r ReceiptReport) {
	fprintf(w, "Receipt %s\n", r.ReceiptID)
	fprintf(w, "  in epoch      : %d (position %d of %d)\n", r.Epoch, r.Index+1, r.Size)
	fprintf(w, "  leaf hash     : %s\n", fullHex(r.LeafHash))
	fprintf(w, "  proof         : %d hashes\n", r.ProofLength)
	fprintf(w, "  chain root    : %s\n", fullHex(r.Report.ChainRoot))
	fprintf(w, "  finality      : %s\n", describeFinality(r.Report.Finality))
	if r.Included {
		fprintln(w, "  RESULT        : INCLUDED: the receipt, as stored now, is inside the root the chain holds")
		return
	}
	fprintf(w, "  RESULT        : NOT INCLUDED: %s\n", r.Report.Detail)
}

func rootOrDash(root [32]byte, ok bool) string {
	if !ok {
		return "-"
	}
	return fullHex(root)
}

func describeFinality(f string) string {
	switch f {
	case FinalityFinalized:
		return "finalized (settled on Ethereum)"
	case FinalitySoft:
		return "soft (confirmed by the sequencer; not yet settled on Ethereum)"
	default:
		return "unknown"
	}
}

func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
func fprintln(w io.Writer, a ...any)               { _, _ = fmt.Fprintln(w, a...) }

// Failed reports whether a verdict means the history cannot be trusted.
func (v Verdict) Failed() bool { return v != VerdictMatch }
