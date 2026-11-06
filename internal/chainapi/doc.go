// Package chainapi is the small HTTP surface for the chain feature: which
// chain and contracts the coordinator uses, and binding an owner's wallet.
//
// It is served on its own listener, started only when the chain extension is
// asked to (VYOMANAUT_CHAIN_API_ADDR), so the coordinator's main API and its
// routing table are untouched. Plain HTTP, for the closed demo network: put it
// behind TLS anywhere else.
//
// Every route that changes or reveals an owner's data needs the owner's normal
// session token. Binding also needs a signature from the wallet itself, so both
// "who is asking" and "who controls the wallet" are proven.
package chainapi
