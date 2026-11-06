package anchor

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderEpochShowsTheVerdictAndBothRoots(t *testing.T) {
	r := EpochReport{Epoch: 3, State: "ANCHORED", Verdict: VerdictMismatch, Detail: "root differs",
		DBRoot: [32]byte{1}, DBLeaves: 4, ChainRoot: [32]byte{2}, ChainLeaves: 4,
		ChainTimestamp: time.Unix(1_700_000_000, 0), AnchorBlock: 77, Finality: FinalitySoft}
	var b bytes.Buffer
	RenderEpoch(&b, r)
	out := b.String()
	for _, want := range []string{"Epoch 3", "MISMATCH", "0x01", "0x02", "block 77", "soft"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
	if !VerdictMismatch.Failed() || !VerdictNotOnChain.Failed() || VerdictMatch.Failed() {
		t.Fatal("only MATCH may count as success")
	}
}

func TestRenderEpochForAnEpochTheChainNeverSaw(t *testing.T) {
	var b bytes.Buffer
	RenderEpoch(&b, EpochReport{Epoch: 9, State: "ANCHORED", Verdict: VerdictNotOnChain, Detail: "no record"})
	if strings.Contains(b.String(), "chain root") {
		t.Fatalf("there is no chain root to show:\n%s", b.String())
	}
}

func TestRenderListAndReceipt(t *testing.T) {
	var b bytes.Buffer
	RenderList(&b, nil)
	if !strings.Contains(b.String(), "no epochs") {
		t.Fatalf("empty list: %q", b.String())
	}
	b.Reset()
	tx := make([]byte, 32)
	RenderList(&b, []EpochSummary{{Epoch: 1, State: "ANCHORED", Leaves: 5, AnchorTx: tx}, {Epoch: 2, State: "RESERVED", Error: "rpc down"}})
	if !strings.Contains(b.String(), "rpc down") || !strings.Contains(b.String(), "ANCHORED") {
		t.Fatalf("list: %q", b.String())
	}
	b.Reset()
	RenderReceipt(&b, ReceiptReport{ReceiptID: "abc", Epoch: 1, Index: 2, Size: 9, Included: true})
	if !strings.Contains(b.String(), "INCLUDED") || strings.Contains(b.String(), "NOT INCLUDED") {
		t.Fatalf("included: %q", b.String())
	}
	b.Reset()
	RenderReceipt(&b, ReceiptReport{ReceiptID: "abc", Report: EpochReport{Detail: "altered"}})
	if !strings.Contains(b.String(), "NOT INCLUDED") {
		t.Fatalf("not included: %q", b.String())
	}
}
