# Contracts

Two small contracts on Whitechain Sepolia (chain ID 1874). Solidity 0.8.30, EVM target `prague`, no external libraries.

| Contract | Job |
|---|---|
| `AuditAnchor` | One 32-byte root per epoch of the coordinator's audit history. A provider can prove a receipt was part of that history, and that it was not rewritten later. |
| `VyomaEscrow` | Holds stablecoin from data owners and pays storage providers from posted periods, so a posted payout survives the coordinator disappearing. |

## How money moves

1. A data owner deposits USDT (`deposit`, or `depositWithPermit` so the owner needs no gas).
2. Each period the coordinator posts a payout list with `postPeriod`: owners to debit, accounts to pay. Debits must equal payouts and no owner can be debited past their credit.
3. A challenge window follows. The guardian, a different key, can `vetoPeriod` and every debit goes back to its owner.
4. After the window anyone can `claim` or `claimMany`. The funds always go to the named account, never the caller. The coordinator normally submits claims and pays the gas, so providers hold no gas token.
5. If the coordinator posts nothing for `silenceTimeout`, anyone can `refund(owner)` to return that owner's unspent credit to them. Periods already posted stay claimable.

There is no admin withdrawal and no upgrade path. The coordinator can only post periods bounded by owner credit. The guardian can only veto inside a window.

## Limitations

- The payout list is stored on chain, so `postPeriod` costs gas linear in the number of accounts paid. That suits tens to low hundreds of providers per period. A very large network would swap the list for a Merkle root behind the same `claim` interface.
- Coordinator and guardian are fixed at deployment. There is no key rotation.
- Owners cannot withdraw unspent credit while the coordinator is active.
- The anchor proves a receipt existed in the coordinator's published history. It does not prove the coordinator reported honestly when it wrote the receipt.

## Test

```bash
cd contracts
forge test              # 46 tests, including 512-run fuzz tests
forge test --gas-report
```

The tests use a local minimal cheat-code interface, so there is nothing to `forge install`.

## Deploy

```bash
cd contracts
export WHITECHAIN_SEPOLIA_RPC_URL=https://rpc.testnet.whitechain.io
export COORDINATOR_ADDR=<coordinator wallet>   GUARDIAN_ADDR=<guardian wallet>
forge script script/Deploy.s.sol:Deploy --rpc-url whitechain_sepolia \
  --private-key "$(cat ~/vyoma-keys/deployer.key)" --broadcast
```

Optional: `CHALLENGE_WINDOW_SECONDS` (default 120) and `SILENCE_TIMEOUT_SECONDS` (default 900).

## Verify on the explorer

The compiler version and settings come from `foundry.toml`. The trailing `/api/` on the URL is required.

```bash
forge verify-contract <ANCHOR_ADDRESS> src/AuditAnchor.sol:AuditAnchor \
  --rpc-url whitechain_sepolia --verifier blockscout \
  --verifier-url https://explorer.testnet.whitechain.io/api/ \
  --constructor-args "$(cast abi-encode 'constructor(address)' $COORDINATOR_ADDR)"

forge verify-contract <ESCROW_ADDRESS> src/VyomaEscrow.sol:VyomaEscrow \
  --rpc-url whitechain_sepolia --verifier blockscout \
  --verifier-url https://explorer.testnet.whitechain.io/api/ \
  --constructor-args "$(cast abi-encode 'constructor(address,address,address,uint64,uint64)' \
    0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7 $COORDINATOR_ADDR $GUARDIAN_ADDR 120 900)"
```

Deployed addresses are recorded in `deployments/whitechain/sepolia.json`.
