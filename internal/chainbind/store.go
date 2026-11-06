package chainbind

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// PostgreSQL error codes this package maps to its own errors.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

// Binder reads and writes the wallet bindings. db is the application role's pool.
type Binder struct{ db *sql.DB }

// NewBinder builds a Binder.
func NewBinder(db *sql.DB) *Binder { return &Binder{db: db} }

// BindOwner binds wallet to owner. Binding the same pair again succeeds with
// already=true; a different wallet for the same owner, or a wallet another owner
// holds, is refused. Binding is never silently replaced: deposits already made
// from the old wallet are credited to the owner who held it.
func (b *Binder) BindOwner(ctx context.Context, owner uuid.UUID, wallet chain.Address) (already bool, err error) {
	if wallet.IsZero() {
		return false, ErrZeroWallet
	}
	res, err := b.db.ExecContext(ctx,
		`INSERT INTO owner_wallets (owner_id, wallet) VALUES ($1, $2) ON CONFLICT (owner_id) DO NOTHING`,
		owner, wallet[:])
	if err != nil {
		var pe *pq.Error
		if errors.As(err, &pe) {
			switch pe.Code {
			case codeUniqueViolation:
				return false, ErrWalletTaken
			case codeForeignKeyViolation:
				return false, ErrUnknownOwner
			}
		}
		return false, fmt.Errorf("bind owner wallet: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return false, nil
	}
	current, found, err := b.OwnerWallet(ctx, owner)
	if err != nil {
		return false, err
	}
	if found && current == wallet {
		return true, nil
	}
	return false, ErrAlreadyBound
}

// OwnerWallet returns the owner's bound wallet, if any.
func (b *Binder) OwnerWallet(ctx context.Context, owner uuid.UUID) (chain.Address, bool, error) {
	var raw []byte
	err := b.db.QueryRowContext(ctx, `SELECT wallet FROM owner_wallets WHERE owner_id = $1`, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return chain.Address{}, false, nil
	}
	if err != nil {
		return chain.Address{}, false, fmt.Errorf("read owner wallet: %w", err)
	}
	var a chain.Address
	if len(raw) != len(a) {
		return chain.Address{}, false, fmt.Errorf("stored wallet is %d bytes", len(raw))
	}
	copy(a[:], raw)
	return a, true, nil
}

// Deriver gives a provider's payout address.
type Deriver func(provider uuid.UUID) (chain.Address, error)

// AssignProviderWallets gives every provider that has not left, and has no
// payout address yet, the address deriver returns. It returns how many it
// assigned. Providers already bound are left alone, so it is safe to run
// repeatedly and never changes an address once given.
func (b *Binder) AssignProviderWallets(ctx context.Context, derive Deriver) (int, error) {
	rows, err := b.db.QueryContext(ctx, `
SELECT p.provider_id FROM providers p
LEFT JOIN provider_payout_addresses a ON a.provider_id = p.provider_id
WHERE a.provider_id IS NULL AND p.status <> 'DEPARTED'
ORDER BY p.provider_id`)
	if err != nil {
		return 0, fmt.Errorf("list providers without a wallet: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	assigned := 0
	for _, id := range ids {
		w, err := derive(id)
		if err != nil {
			return assigned, fmt.Errorf("derive a wallet: %w", err)
		}
		res, err := b.db.ExecContext(ctx,
			`INSERT INTO provider_payout_addresses (provider_id, wallet) VALUES ($1, $2) ON CONFLICT (provider_id) DO NOTHING`,
			id, w[:])
		if err != nil {
			return assigned, fmt.Errorf("assign a wallet: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 1 {
			assigned++
		}
	}
	return assigned, nil
}
