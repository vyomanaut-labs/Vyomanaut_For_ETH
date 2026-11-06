package chainapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainbind"
)

const (
	maxBodyBytes = 4 << 10
	ownerRole    = "owner"
)

// Wallets is what the handlers need to store bindings. *chainbind.Binder
// satisfies it.
type Wallets interface {
	BindOwner(ctx context.Context, owner uuid.UUID, wallet chain.Address) (already bool, err error)
	OwnerWallet(ctx context.Context, owner uuid.UUID) (chain.Address, bool, error)
}

// Verifier checks a bearer token and returns who it names and in what role.
type Verifier func(token string) (subject uuid.UUID, role string, err error)

// Info describes the chain setup, so a client needs only the coordinator's
// address and not the contract addresses.
type Info struct {
	ChainID  uint64
	Escrow   chain.Address
	Token    chain.Address
	Anchor   chain.Address // zero when anchoring is off
	Explorer string        // optional base URL of a block explorer
}

// Deps are the handler's dependencies.
type Deps struct {
	Wallets Wallets
	Verify  Verifier
	Info    Info
}

// New returns the handler.
func New(d Deps) http.Handler {
	h := &handler{d: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chain/v1/info", h.info)
	mux.HandleFunc("GET /chain/v1/wallet", h.owner(h.getWallet))
	mux.HandleFunc("GET /chain/v1/wallet/message", h.owner(h.message))
	mux.HandleFunc("POST /chain/v1/wallet", h.owner(h.bind))
	return mux
}

type handler struct{ d Deps }

type errorBody struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Code: code, Message: msg})
}

// owner wraps a handler so it runs only for a valid owner session.
func (h *handler) owner(next func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const scheme = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, scheme) {
			fail(w, http.StatusUnauthorized, "unauthorized", "send the owner's session token as a Bearer token")
			return
		}
		subject, role, err := h.d.Verify(strings.TrimSpace(strings.TrimPrefix(auth, scheme)))
		if err != nil || role != ownerRole {
			fail(w, http.StatusUnauthorized, "unauthorized", "the session token is missing, expired or not an owner's")
			return
		}
		next(w, r, subject)
	}
}

func (h *handler) info(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{
		"chain_id": h.d.Info.ChainID,
		"escrow":   h.d.Info.Escrow.String(),
		"token":    h.d.Info.Token.String(),
	}
	if !h.d.Info.Anchor.IsZero() {
		out["anchor"] = h.d.Info.Anchor.String()
	}
	if h.d.Info.Explorer != "" {
		out["explorer"] = h.d.Info.Explorer
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) getWallet(w http.ResponseWriter, r *http.Request, owner uuid.UUID) {
	wallet, found, err := h.d.Wallets.OwnerWallet(r.Context(), owner)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal", "could not read the wallet binding")
		return
	}
	out := map[string]any{"owner_id": owner.String(), "wallet": nil}
	if found {
		out["wallet"] = wallet.String()
	}
	writeJSON(w, http.StatusOK, out)
}

// message returns the exact text to sign for a wallet, for tools that sign by
// hand (for example `cast wallet sign`).
func (h *handler) message(w http.ResponseWriter, r *http.Request, owner uuid.UUID) {
	wallet, err := chain.ParseAddress(r.URL.Query().Get("wallet"))
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_wallet", "wallet must be a 0x address")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": chainbind.BindingMessage(h.d.Info.ChainID, owner, wallet),
	})
}

type bindRequest struct {
	Wallet    string `json:"wallet"`
	Signature string `json:"signature"`
}

func (h *handler) bind(w http.ResponseWriter, r *http.Request, owner uuid.UUID) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req bindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "the body must be JSON with wallet and signature")
		return
	}
	wallet, err := chain.ParseAddress(req.Wallet)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_wallet", "wallet must be a 0x address")
		return
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(req.Signature), "0x"))
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_signature", "the signature must be hex")
		return
	}
	sig, err := chain.ParseSignature(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_signature", "the signature must be 65 bytes (r, s, v)")
		return
	}
	if err := chainbind.VerifyBinding(h.d.Info.ChainID, owner, wallet, sig); err != nil {
		fail(w, http.StatusForbidden, "bad_signature", "that signature does not prove control of the wallet; sign the message from GET /chain/v1/wallet/message")
		return
	}

	already, err := h.d.Wallets.BindOwner(r.Context(), owner, wallet)
	switch {
	case errors.Is(err, chainbind.ErrAlreadyBound):
		fail(w, http.StatusConflict, "already_bound", "this owner already has a different wallet bound")
	case errors.Is(err, chainbind.ErrWalletTaken):
		fail(w, http.StatusConflict, "wallet_taken", "that wallet is already bound to another owner")
	case errors.Is(err, chainbind.ErrUnknownOwner):
		fail(w, http.StatusNotFound, "unknown_owner", "no such owner")
	case err != nil:
		fail(w, http.StatusInternalServerError, "internal", "could not save the wallet binding")
	default:
		status := "bound"
		if already {
			status = "already_bound"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"owner_id": owner.String(), "wallet": wallet.String(), "chain_id": h.d.Info.ChainID, "status": status,
		})
	}
}
