// Package chainctl is the logic behind the vyomachain command: making a wallet
// key, binding it to an owner, depositing USDT into the escrow, and the two
// "the coordinator is gone" moves, claiming a posted payout and taking back
// unspent credit.
//
// It talks to the chain directly through internal/chain, so the escape-hatch
// moves work with nothing but an RPC endpoint and a wallet that holds a little
// gas token: no coordinator is involved, which is the point of them.
package chainctl
