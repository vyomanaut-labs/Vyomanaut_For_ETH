// Package chainbind records which chain wallet belongs to whom.
//
// Owners bring their own wallet. An owner proves control of it by signing a
// short, exact message with the wallet (EIP-191 personal_sign); the coordinator
// recovers the signer and binds the wallet to that owner only if the two match.
// Which owner is asking is established separately, by the owner's normal
// session token, so a stranger cannot attach a wallet to somebody else's
// account and an owner cannot claim a wallet they do not control.
//
// Providers bring nothing. A provider's payout address is derived by the
// coordinator from its own secret, so a provider needs no wallet, no tokens and
// no new step. That makes the demo's provider wallets custodial: the
// coordinator can reproduce their keys. This is stated plainly in the README;
// it is a demo shortcut, and a real provider would register their own address.
package chainbind
