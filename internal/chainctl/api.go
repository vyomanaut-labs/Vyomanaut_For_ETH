package chainctl

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainbind"
)

const maxReply = 1 << 16

// ChainInfo is what the coordinator's chain API reports.
type ChainInfo struct {
	ChainID  uint64        `json:"chain_id"`
	Escrow   chain.Address `json:"-"`
	Token    chain.Address `json:"-"`
	Anchor   chain.Address `json:"-"`
	Explorer string        `json:"explorer"`
}

// APIError is an error reply from the coordinator.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("the coordinator answered %d", e.Status)
	}
	return fmt.Sprintf("the coordinator refused (%s): %s", e.Code, e.Message)
}

// Client talks to the coordinator's chain API.
type Client struct {
	Base string
	HTTP *http.Client
}

func (c *Client) do(ctx context.Context, method, path, token string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the coordinator's chain API at %s: %w", c.Base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var eb struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &eb)
		return &APIError{Status: resp.StatusCode, Code: eb.Error, Message: eb.Message}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// FetchInfo reads which chain and contracts the coordinator uses.
func (c *Client) FetchInfo(ctx context.Context) (ChainInfo, error) {
	var raw struct {
		ChainID  uint64 `json:"chain_id"`
		Escrow   string `json:"escrow"`
		Token    string `json:"token"`
		Anchor   string `json:"anchor"`
		Explorer string `json:"explorer"`
	}
	if err := c.do(ctx, http.MethodGet, "/chain/v1/info", "", nil, &raw); err != nil {
		return ChainInfo{}, err
	}
	info := ChainInfo{ChainID: raw.ChainID, Explorer: raw.Explorer}
	var err error
	if info.Escrow, err = chain.ParseAddress(raw.Escrow); err != nil {
		return info, errors.New("the coordinator reported a bad escrow address")
	}
	if info.Token, err = chain.ParseAddress(raw.Token); err != nil {
		return info, errors.New("the coordinator reported a bad token address")
	}
	if raw.Anchor != "" {
		if info.Anchor, err = chain.ParseAddress(raw.Anchor); err != nil {
			return info, errors.New("the coordinator reported a bad anchor address")
		}
	}
	return info, nil
}

// BindResult is the coordinator's answer to a binding.
type BindResult struct {
	OwnerID uuid.UUID
	Wallet  chain.Address
	Already bool
}

// BindWallet proves control of key's wallet by signing the binding message and
// asks the coordinator to bind it to the owner named by identity.
func (c *Client) BindWallet(ctx context.Context, id Identity, chainID uint64, key *chain.Key) (BindResult, error) {
	wallet := key.Address()
	msg := chainbind.BindingMessage(chainID, id.OwnerID, wallet)
	sig := key.Sign(chain.PersonalMessageHash([]byte(msg)))
	req := map[string]string{"wallet": wallet.Hex(), "signature": "0x" + hex.EncodeToString(sig.Bytes())}

	var reply struct {
		OwnerID string `json:"owner_id"`
		Status  string `json:"status"`
	}
	if err := c.do(ctx, http.MethodPost, "/chain/v1/wallet", id.Token, req, &reply); err != nil {
		return BindResult{}, err
	}
	return BindResult{OwnerID: id.OwnerID, Wallet: wallet, Already: reply.Status == "already_bound"}, nil
}

// BoundWallet asks which wallet the coordinator has for the owner, if any.
func (c *Client) BoundWallet(ctx context.Context, id Identity) (chain.Address, bool, error) {
	var reply struct {
		Wallet *string `json:"wallet"`
	}
	if err := c.do(ctx, http.MethodGet, "/chain/v1/wallet", id.Token, nil, &reply); err != nil {
		return chain.Address{}, false, err
	}
	if reply.Wallet == nil {
		return chain.Address{}, false, nil
	}
	a, err := chain.ParseAddress(*reply.Wallet)
	if err != nil {
		return chain.Address{}, false, errors.New("the coordinator reported a bad wallet address")
	}
	return a, true, nil
}
