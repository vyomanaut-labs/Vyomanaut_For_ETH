package chain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This test deploys the real contracts to a local Anvil chain and drives every
// operation through the client. It is skipped unless `anvil` is installed and
// the contracts have been built (`cd contracts && forge build`).

const usdt1 = 1_000_000 // one whole token at 6 decimals

type localChain struct {
	rpc *RPC
}

func startAnvil(t *testing.T) *localChain {
	t.Helper()
	bin, err := exec.LookPath("anvil")
	if err != nil {
		t.Skip("anvil is not installed (install Foundry to run the chain integration test)")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, isTCP := l.Addr().(*net.TCPAddr)
	if !isTCP {
		t.Fatal("expected a TCP listener")
	}
	port := addr.Port
	_ = l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--port", fmt.Sprint(port), "--silent")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })

	rpc := NewRPC(fmt.Sprintf("http://127.0.0.1:%d", port), 10*time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := rpc.ChainID(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("anvil did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return &localChain{rpc: rpc}
}

func (c *localChain) raw(t *testing.T, method string, params ...any) {
	t.Helper()
	if err := c.rpc.call(context.Background(), method, params, nil); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

// fundedKey returns a fresh random key holding 100 native tokens.
func (c *localChain) fundedKey(t *testing.T) *Key {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	k, err := ParseKeyHex(hex.EncodeToString(b[:]))
	if err != nil {
		t.Fatal(err)
	}
	c.raw(t, "anvil_setBalance", k.Address().Hex(), "0x56bc75e2d63100000") // 100 ether
	return k
}

func (c *localChain) sender(t *testing.T, k *Key) *Sender {
	t.Helper()
	s, err := NewSender(context.Background(), c.rpc, k)
	if err != nil {
		t.Fatal(err)
	}
	s.PollInterval = 20 * time.Millisecond
	return s
}

func (c *localChain) warp(t *testing.T, seconds int) {
	t.Helper()
	c.raw(t, "evm_increaseTime", seconds)
	c.raw(t, "evm_mine")
}

func artifactBytecode(t *testing.T, rel string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "contracts", "out", rel)
	raw, err := os.ReadFile(path) //nolint:gosec // fixed test path
	if err != nil {
		t.Skipf("contract artifacts missing (%v); run `cd contracts && forge build`", err)
	}
	var art struct {
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
	}
	if err := json.Unmarshal(raw, &art); err != nil {
		t.Fatal(err)
	}
	return mustHexBytes(t, art.Bytecode.Object)
}

func deploy(t *testing.T, s *Sender, code []byte, ctorArgs ...abiArg) Address {
	t.Helper()
	args, err := encodeArgs(ctorArgs)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := s.Send(context.Background(), nil, append(append([]byte{}, code...), args...))
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if rc.ContractAddress == nil {
		t.Fatal("deployment receipt has no contract address")
	}
	return *rc.ContractAddress
}

func mustBalance(t *testing.T, tok *ERC20, who Address) int64 {
	t.Helper()
	v, err := tok.BalanceOf(context.Background(), who)
	if err != nil {
		t.Fatal(err)
	}
	return v.Int64()
}

// receiptOK returns a function that fails the test on error and otherwise
// passes the receipt through, so calls can be wrapped as ok(contract.Method()).
func receiptOK(t *testing.T) func(*Receipt, error) *Receipt {
	return func(rc *Receipt, err error) *Receipt {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return rc
	}
}

func TestAgainstALocalChain(t *testing.T) {
	ctx := context.Background()
	usdtCode := artifactBytecode(t, "Mocks.sol/MockUSDT.json")
	escrowCode := artifactBytecode(t, "VyomaEscrow.sol/VyomaEscrow.json")
	anchorCode := artifactBytecode(t, "AuditAnchor.sol/AuditAnchor.json")
	c := startAnvil(t)
	ok := receiptOK(t)

	coordKey, guardKey, ownerKey := c.fundedKey(t), c.fundedKey(t), c.fundedKey(t)
	coord, owner := c.sender(t, coordKey), c.sender(t, ownerKey)
	p1, p2 := c.fundedKey(t).Address(), c.fundedKey(t).Address()

	// deploy: token, escrow (window 120 s, silence 900 s) and anchor
	tokenAddr := deploy(t, coord, usdtCode)
	escrowAddr := deploy(t, coord, escrowCode, addrArg(tokenAddr), addrArg(coordKey.Address()),
		addrArg(guardKey.Address()), uint64Arg(120), uint64Arg(900))
	anchorAddr := deploy(t, coord, anchorCode, addrArg(coordKey.Address()))

	tokenAsCoord := NewERC20(tokenAddr, c.rpc, coord)
	tokenAsOwner := NewERC20(tokenAddr, c.rpc, owner)
	escrowAsCoord := NewEscrowContract(escrowAddr, c.rpc, coord)
	escrowAsOwner := NewEscrowContract(escrowAddr, c.rpc, owner)
	anchor := NewAnchorContract(anchorAddr, c.rpc, coord)

	// the mock token lets anyone mint; give the owner 1,000 tokens
	mint, err := encodeCall("mint(address,uint256)", addrArg(ownerKey.Address()), uintArg(big.NewInt(1000*usdt1)))
	if err != nil {
		t.Fatal(err)
	}
	ok(mustSend(coord, &tokenAddr, mint))

	t.Run("deposit", func(t *testing.T) {
		ok := receiptOK(t)
		ok(tokenAsOwner.Approve(ctx, escrowAddr, big.NewInt(500*usdt1)))
		if al, _ := tokenAsOwner.Allowance(ctx, ownerKey.Address(), escrowAddr); al.Int64() != 500*usdt1 {
			t.Fatalf("allowance = %v", al)
		}
		rc := ok(escrowAsOwner.Deposit(ctx, big.NewInt(500*usdt1)))

		credit, err := escrowAsOwner.Credit(ctx, ownerKey.Address())
		if err != nil || credit.Int64() != 500*usdt1 {
			t.Fatalf("credit = %v, %v", credit, err)
		}
		events, err := escrowAsOwner.DepositedEvents(ctx, 0, rc.BlockNumber)
		if err != nil || len(events) != 1 {
			t.Fatalf("events = %v, %v", events, err)
		}
		ev := events[0]
		if ev.Owner != ownerKey.Address() || ev.Amount.Int64() != 500*usdt1 || ev.TxHash != rc.TxHash {
			t.Fatalf("event = %+v", ev)
		}
		if mustBalance(t, tokenAsOwner, ownerKey.Address()) != 500*usdt1 {
			t.Fatal("owner should have 500 left")
		}
	})

	t.Run("a stranger cannot post a period", func(t *testing.T) {
		_, err := NewEscrowContract(escrowAddr, c.rpc, owner).PostPeriod(ctx, 1, [32]byte{},
			[]Address{ownerKey.Address()}, []*big.Int{big.NewInt(usdt1)},
			[]Address{ownerKey.Address()}, []*big.Int{big.NewInt(usdt1)})
		var re *RevertError
		if !errors.Is(err, ErrReverted) || !errors.As(err, &re) || re.Reason != "NotCoordinator()" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("posting more than the credit is refused with a readable reason", func(t *testing.T) {
		_, err := escrowAsCoord.PostPeriod(ctx, 1, [32]byte{},
			[]Address{ownerKey.Address()}, []*big.Int{big.NewInt(501 * usdt1)},
			[]Address{p1}, []*big.Int{big.NewInt(501 * usdt1)})
		var re *RevertError
		if !errors.As(err, &re) || !strings.HasPrefix(re.Reason, "InsufficientCredit(address,uint256,uint256)") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("post, wait out the window, claim", func(t *testing.T) {
		ok := receiptOK(t)
		ok(escrowAsCoord.PostPeriod(ctx, 1, [32]byte{1},
			[]Address{ownerKey.Address()}, []*big.Int{big.NewInt(100 * usdt1)},
			[]Address{p1, p2}, []*big.Int{big.NewInt(60 * usdt1), big.NewInt(40 * usdt1)}))

		if last, _ := escrowAsCoord.LastPeriod(ctx); last != 1 {
			t.Fatalf("last period = %d", last)
		}
		info, err := escrowAsCoord.PeriodInfo(ctx, 1)
		if err != nil || info.Total.Int64() != 100*usdt1 || info.Vetoed || info.ClaimableAt == 0 {
			t.Fatalf("info = %+v, %v", info, err)
		}
		if al, _ := escrowAsCoord.AllocationOf(ctx, 1, p1); al.Int64() != 60*usdt1 {
			t.Fatalf("allocation = %v", al)
		}

		// too early
		_, err = escrowAsCoord.ClaimMany(ctx, 1, []Address{p1, p2})
		var re *RevertError
		if !errors.As(err, &re) || !strings.HasPrefix(re.Reason, "WindowStillOpen") {
			t.Fatalf("early claim: %v", err)
		}

		c.warp(t, 130)
		ok(escrowAsCoord.ClaimMany(ctx, 1, []Address{p1, p2}))
		if mustBalance(t, tokenAsCoord, p1) != 60*usdt1 || mustBalance(t, tokenAsCoord, p2) != 40*usdt1 {
			t.Fatal("providers were not paid the posted amounts")
		}
		if un, _ := escrowAsCoord.TotalUnclaimed(ctx); un.Sign() != 0 {
			t.Fatalf("unclaimed = %v", un)
		}

		// claiming again is refused
		if _, err = escrowAsCoord.Claim(ctx, 1, p1); !errors.Is(err, ErrReverted) {
			t.Fatalf("double claim: %v", err)
		}
	})

	t.Run("anchor an audit root", func(t *testing.T) {
		ok := receiptOK(t)
		root := mustRoot(t)
		ok(anchor.Publish(ctx, 1, root, 8))
		rec, err := anchor.Roots(ctx, 1)
		if err != nil || rec.Root != root || rec.LeafCount != 8 || rec.Timestamp == 0 {
			t.Fatalf("record = %+v, %v", rec, err)
		}
		if last, _ := anchor.LastEpoch(ctx); last != 1 {
			t.Fatalf("last epoch = %d", last)
		}
		_, err = anchor.Publish(ctx, 1, root, 8) // the same epoch again
		var re *RevertError
		if !errors.As(err, &re) || !strings.HasPrefix(re.Reason, "EpochNotIncreasing") {
			t.Fatalf("repeat epoch: %v", err)
		}
		if none, _ := anchor.Roots(ctx, 99); none.Timestamp != 0 {
			t.Fatal("an unanchored epoch must read as empty")
		}
	})

	t.Run("the coordinator disappears and the owner is refunded", func(t *testing.T) {
		ok := receiptOK(t)
		_, err := escrowAsOwner.Refund(ctx, ownerKey.Address()) // too early
		var re *RevertError
		if !errors.As(err, &re) || !strings.HasPrefix(re.Reason, "CoordinatorStillActive") {
			t.Fatalf("early refund: %v", err)
		}
		c.warp(t, 1000)
		// anyone can submit it; the money goes to the owner
		stranger := c.sender(t, c.fundedKey(t))
		ok(NewEscrowContract(escrowAddr, c.rpc, stranger).Refund(ctx, ownerKey.Address()))
		// 1000 minted - 500 deposited + 400 unspent credit
		if got := mustBalance(t, tokenAsOwner, ownerKey.Address()); got != 900*usdt1 {
			t.Fatalf("owner balance = %d, want %d", got, 900*usdt1)
		}
		if mustBalance(t, tokenAsOwner, escrowAddr) != 0 {
			t.Fatal("escrow should be empty")
		}
	})

	t.Run("concurrent sends never collide on a nonce", func(t *testing.T) {
		const n = 8
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := tokenAsOwner.Transfer(ctx, p1, big.NewInt(usdt1))
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("a concurrent send failed: %v", err)
			}
		}
		if got := mustBalance(t, tokenAsOwner, p1); got != 60*usdt1+n*usdt1 {
			t.Fatalf("p1 balance = %d", got)
		}
	})

	t.Run("a node without finality reports it", func(t *testing.T) {
		if _, err := c.rpc.FinalizedBlock(ctx); err != nil && !errors.Is(err, ErrNoFinality) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func mustSend(s *Sender, to *Address, data []byte) (*Receipt, error) {
	return s.Send(context.Background(), to, data)
}
