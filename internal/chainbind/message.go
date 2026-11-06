package chainbind

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// Errors.
var (
	// ErrBadSignature means the signature does not come from the wallet.
	ErrBadSignature = errors.New("chainbind: the signature does not come from that wallet")
	// ErrAlreadyBound means the owner already has a different wallet.
	ErrAlreadyBound = errors.New("chainbind: this owner already has a different wallet bound")
	// ErrWalletTaken means another owner already uses the wallet.
	ErrWalletTaken = errors.New("chainbind: that wallet is already bound to another owner")
	// ErrUnknownOwner means no such owner is registered.
	ErrUnknownOwner = errors.New("chainbind: unknown owner")
	// ErrZeroWallet means the wallet is the zero address.
	ErrZeroWallet = errors.New("chainbind: the zero address cannot be bound")
)

// BindingMessage is the exact text an owner signs. It names the owner, the
// wallet and the chain, so a signature cannot be replayed for another owner,
// another wallet or another network.
func BindingMessage(chainID uint64, owner uuid.UUID, wallet chain.Address) string {
	return fmt.Sprintf("Vyomanaut wallet binding v1\nowner: %s\nwallet: %s\nchain: %d", owner, wallet.Hex(), chainID)
}

// VerifyBinding checks that sig is the wallet's signature over the binding
// message for owner.
func VerifyBinding(chainID uint64, owner uuid.UUID, wallet chain.Address, sig chain.Signature) error {
	if wallet.IsZero() {
		return ErrZeroWallet
	}
	digest := chain.PersonalMessageHash([]byte(BindingMessage(chainID, owner, wallet)))
	got, err := chain.RecoverAddress(digest, sig)
	if err != nil || got != wallet {
		return ErrBadSignature
	}
	return nil
}
