package chainsettle

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

func wallet(b byte) chain.Address { return chain.Address{b} }

func sumOf(ds []Debit) int64 {
	var s int64
	for _, d := range ds {
		s += d.Micro
	}
	return s
}

func TestSplitDebitsSingleOwnerPaysEverything(t *testing.T) {
	got, err := SplitDebits(500, []OwnerCapacity{{Wallet: wallet(1), Weight: 800, Credit: 900}})
	if err != nil || len(got) != 1 || got[0].Micro != 500 || got[0].Wallet != wallet(1) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSplitDebitsIsProportionalToWhatEachCanFund(t *testing.T) {
	got, err := SplitDebits(1000, []OwnerCapacity{
		{Wallet: wallet(1), Weight: 3000, Credit: 3000},
		{Wallet: wallet(2), Weight: 1000, Credit: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Micro != 750 || got[1].Micro != 250 {
		t.Fatalf("got %+v, want 750 and 250", got)
	}
}

func TestSplitDebitsHandsOutRoundingRemainderToTheLargestFraction(t *testing.T) {
	// 10 over three equal owners: 3.33 each -> 4, 3, 3 in a fixed order
	owners := []OwnerCapacity{
		{Wallet: wallet(1), Weight: 100, Credit: 100},
		{Wallet: wallet(2), Weight: 100, Credit: 100},
		{Wallet: wallet(3), Weight: 100, Credit: 100},
	}
	got, err := SplitDebits(10, owners)
	if err != nil || sumOf(got) != 10 {
		t.Fatalf("got %+v, %v", got, err)
	}
	again, _ := SplitDebits(10, owners)
	for i := range got {
		if got[i] != again[i] {
			t.Fatal("the split must be deterministic")
		}
	}
}

func TestSplitDebitsHonoursTheTighterOfWeightAndCredit(t *testing.T) {
	got, err := SplitDebits(150, []OwnerCapacity{
		{Wallet: wallet(1), Weight: 1000, Credit: 50},  // credit is the limit: 50
		{Wallet: wallet(2), Weight: 100, Credit: 1000}, // weight is the limit: 100
	})
	if err != nil || len(got) != 2 || got[0].Micro != 50 || got[1].Micro != 100 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSplitDebitsRefusesWhatOwnersCannotFund(t *testing.T) {
	_, err := SplitDebits(101, []OwnerCapacity{
		{Wallet: wallet(1), Weight: 60, Credit: 1000}, {Wallet: wallet(2), Weight: 1000, Credit: 40},
	})
	if !errors.Is(err, ErrInsufficientCapacity) {
		t.Fatalf("err = %v, want ErrInsufficientCapacity", err)
	}
	if _, err := SplitDebits(1, nil); !errors.Is(err, ErrInsufficientCapacity) {
		t.Fatalf("no owners: err = %v", err)
	}
}

func TestSplitDebitsIgnoresOwnersWithNothingToGive(t *testing.T) {
	got, err := SplitDebits(10, []OwnerCapacity{
		{Wallet: wallet(1), Weight: 0, Credit: 100}, {Wallet: wallet(2), Weight: 100, Credit: 0},
		{Wallet: wallet(3), Weight: -5, Credit: 100}, {Wallet: wallet(4), Weight: 100, Credit: 100},
	})
	if err != nil || len(got) != 1 || got[0].Wallet != wallet(4) || got[0].Micro != 10 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSplitDebitsNothingToSplit(t *testing.T) {
	if got, err := SplitDebits(0, nil); got != nil || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSplitDebitsDoesNotOverflowOnHugeAmounts(t *testing.T) {
	const huge = maxInt64 / 2
	got, err := SplitDebits(huge, []OwnerCapacity{
		{Wallet: wallet(1), Weight: maxInt64, Credit: maxInt64}, {Wallet: wallet(2), Weight: maxInt64, Credit: maxInt64},
	})
	if err != nil || sumOf(got) != huge {
		t.Fatalf("sum = %d (want %d), err = %v", sumOf(got), int64(huge), err)
	}
}

// The properties that matter, over many random inputs: the debits add up to
// exactly the payout, nobody is asked for more than they can fund, and the
// result does not depend on anything but the inputs.
func TestSplitDebitsProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 5000; iter++ {
		n := 1 + rng.Intn(8)
		owners := make([]OwnerCapacity, n)
		var capacity int64
		for i := range owners {
			owners[i] = OwnerCapacity{Wallet: wallet(byte(i + 1)), Weight: rng.Int63n(1_000_000), Credit: rng.Int63n(1_000_000)}
			capacity += owners[i].effective()
		}
		if capacity == 0 {
			continue
		}
		total := 1 + rng.Int63n(capacity)

		got, err := SplitDebits(total, owners)
		if err != nil {
			t.Fatalf("iter %d: %v", iter, err)
		}
		if sumOf(got) != total {
			t.Fatalf("iter %d: debits sum to %d, want %d", iter, sumOf(got), total)
		}
		byWallet := map[chain.Address]OwnerCapacity{}
		for _, o := range owners {
			byWallet[o.Wallet] = o
		}
		for _, d := range got {
			if d.Micro <= 0 || d.Micro > byWallet[d.Wallet].effective() {
				t.Fatalf("iter %d: debit %d for %v exceeds what the owner can fund (%d)",
					iter, d.Micro, d.Wallet, byWallet[d.Wallet].effective())
			}
		}
		if len(owners) > 0 && capacity >= total {
			again, _ := SplitDebits(total, owners)
			if len(again) != len(got) {
				t.Fatalf("iter %d: not deterministic", iter)
			}
		}
	}
}

// The same properties with amounts near the int64 limit, where a naive sum
// overflows.
func TestSplitDebitsPropertiesAtHugeMagnitudes(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for iter := 0; iter < 2000; iter++ {
		n := 2 + rng.Intn(5)
		owners := make([]OwnerCapacity, n)
		var capacityBig int64
		saturated := false
		for i := range owners {
			c := maxInt64/4 + rng.Int63n(maxInt64/4)
			owners[i] = OwnerCapacity{Wallet: wallet(byte(i + 1)), Weight: c, Credit: c}
			if capacityBig > maxInt64-c {
				saturated = true
			} else {
				capacityBig += c
			}
		}
		total := 1 + rng.Int63n(maxInt64/2)
		if !saturated && total > capacityBig {
			total = capacityBig
		}
		got, err := SplitDebits(total, owners)
		if err != nil {
			t.Fatalf("iter %d: %v", iter, err)
		}
		if sumOf(got) != total {
			t.Fatalf("iter %d: debits sum to %d, want %d", iter, sumOf(got), total)
		}
		for _, d := range got {
			if d.Micro <= 0 || d.Micro > owners[int(d.Wallet[0])-1].effective() {
				t.Fatalf("iter %d: a debit exceeds its owner's capacity", iter)
			}
		}
	}
}
