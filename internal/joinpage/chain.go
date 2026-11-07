package joinpage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// The deployment Vyomanaut Labs runs on Whitechain Sepolia
// (deployments/whitechain/sepolia.json). The desk shows it as a reference
// when this network runs without chains.
const (
	refChainName   = "Whitechain Sepolia"
	refChainID     = 1874
	refExplorer    = "https://explorer.testnet.whitechain.io"
	refEscrow      = "0xBcE841C78c6c40C551fc223Ba9415D4751Af5958"
	refAnchor      = "0x1b2b623ecA0A7899c6a24eaf401a99d2434FDB3d"
	refUSDT        = "0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7"
	refCoordinator = "0xc86553A4fC45352Bd79F1A45a0c338E598B49888"
	refGuardian    = "0xf4d6D323970cc80DC39461b29A272b34c4767cb9"

	arkivName        = "Arkiv Rabdi testnet"
	arkivChainID     = 7794095
	arkivExplorer    = "https://rabdi.explorer.arkiv.network"
	arkivDefaultTag  = "vyomanaut-demo"
	refArkivWriter   = "0x1044E36804C0cED0243529dc6Dc865a3E0b727B9"
	arkivSystemAddr  = "0x4400000000000000000000000000000000000044"
	chainFeedPerKind = 15
	chainFeedMax     = 40
)

// ChainConfig is what the desk's on-chain section shows. Live = this network
// actually writes to that chain; otherwise the reference deployment is shown.
type ChainConfig struct {
	WhitechainLive bool
	ChainID        int64
	Explorer       string
	Escrow         string
	Anchor         string
	Coordinator    string // the coordinator's wallet

	ArkivLive   bool
	ArkivWriter string
	ArkivAppTag string
}

// ChainFromEnv reads the same VYOMANAUT_CHAIN_* and VYOMANAUT_ARKIV_*
// settings the coordinator runs with. Keys are read only to learn their
// public address.
func ChainFromEnv() ChainConfig {
	c := ChainConfig{
		WhitechainLive: os.Getenv("VYOMANAUT_CHAIN_ENABLED") == "1",
		Explorer:       strings.TrimRight(os.Getenv("VYOMANAUT_CHAIN_EXPLORER_URL"), "/"),
		Escrow:         os.Getenv("VYOMANAUT_CHAIN_ESCROW_ADDRESS"),
		Anchor:         os.Getenv("VYOMANAUT_CHAIN_ANCHOR_ADDRESS"),
		ArkivLive:      os.Getenv("VYOMANAUT_ARKIV_ENABLED") == "1",
		ArkivAppTag:    os.Getenv("VYOMANAUT_ARKIV_APP_TAG"),
	}
	c.ChainID, _ = strconv.ParseInt(os.Getenv("VYOMANAUT_CHAIN_ID"), 10, 64)
	if k, err := chain.LoadKeyFile(os.Getenv("VYOMANAUT_CHAIN_PRIVATE_KEY_FILE")); err == nil {
		c.Coordinator = k.Address().String()
	}
	if k, err := chain.LoadKeyFile(os.Getenv("VYOMANAUT_ARKIV_PRIVATE_KEY_FILE")); err == nil {
		c.ArkivWriter = k.Address().String()
	}
	return c
}

// ChainLink is one thing to open on an explorer.
type ChainLink struct {
	Label string `json:"label"`
	What  string `json:"what"`
	URL   string `json:"url"`
	Value string `json:"value,omitempty"`
}

// ChainSection is one chain on the desk.
type ChainSection struct {
	Name     string      `json:"name"`
	ChainID  int64       `json:"chain_id"`
	Live     bool        `json:"live"`
	Explorer string      `json:"explorer"`
	Role     string      `json:"role"`
	Links    []ChainLink `json:"links"`
	Query    string      `json:"query,omitempty"`
}

// ChainEvent is one transaction this network made, newest first.
type ChainEvent struct {
	At     time.Time `json:"at"`
	Chain  string    `json:"chain"` // "whitechain" or "arkiv"
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	TxURL  string    `json:"tx_url"`
}

func (c ChainConfig) sections() []ChainSection {
	exp := c.Explorer
	if exp == "" {
		exp = refExplorer
	}
	escrow, anchor, coord := orDefault(c.Escrow, refEscrow), orDefault(c.Anchor, refAnchor), orDefault(c.Coordinator, refCoordinator)
	id := c.ChainID
	if id == 0 {
		id = refChainID
	}
	addr := func(a string) string { return exp + "/address/" + a }
	white := ChainSection{
		Name: refChainName, ChainID: id, Live: c.WhitechainLive, Explorer: exp,
		Role: "Money and proof. Owners pay test USDT into an escrow contract; providers are paid from it in periods, and owners can take back unspent credit if the coordinator goes silent. Every audit period's results are also sealed here as one Merkle root.",
		Links: []ChainLink{
			{Label: "Escrow contract", What: "Deposits, payout periods, claims and refunds (events Deposited, PeriodPosted, Claimed, Refunded)", URL: addr(escrow), Value: escrow},
			{Label: "Audit anchor contract", What: "One Merkle root of audit receipts per epoch (event Anchored)", URL: addr(anchor), Value: anchor},
			{Label: "Coordinator wallet", What: "Sends the period and anchor transactions", URL: addr(coord), Value: coord},
			{Label: "Test USDT", What: "The token owners pay in", URL: addr(refUSDT), Value: refUSDT},
			{Label: "Guardian", What: "A separate key that can veto a bad payout period", URL: addr(refGuardian), Value: refGuardian},
		},
	}
	writer := orDefault(c.ArkivWriter, refArkivWriter)
	tag := orDefault(c.ArkivAppTag, arkivDefaultTag)
	ark := ChainSection{
		Name: arkivName, ChainID: arkivChainID, Live: c.ArkivLive, Explorer: arkivExplorer,
		Role:  "A public record anyone can read. Every audit result is published as an entity, and each stored file gets a manifest (and a tombstone when deleted). No owner, file name or content goes on chain.",
		Query: "app = str('" + tag + "') AND kind = str('audit_batch')",
		Links: []ChainLink{
			{Label: "Our Arkiv writer wallet", What: "Every entity this network publishes comes from here", URL: arkivExplorer + "/address/" + writer, Value: writer},
			{Label: "Arkiv entity registry", What: "The system address entities are written to", URL: arkivExplorer + "/address/" + arkivSystemAddr, Value: arkivSystemAddr},
			{Label: "Rabdi explorer", What: "Browse blocks and transactions", URL: arkivExplorer},
		},
	}
	return []ChainSection{white, ark}
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

// chainEvents reads this network's recent on-chain transactions from the
// tables the chain and Arkiv extensions keep. A missing table (an older
// database) just gives no rows.
func (c ChainConfig) chainEvents(ctx context.Context, db *sql.DB) []ChainEvent {
	if db == nil {
		return nil
	}
	exp := orDefault(c.Explorer, refExplorer)
	wtx := func(h []byte) string { return exp + "/tx/0x" + hex.EncodeToString(h) }
	atx := func(h []byte) string { return arkivExplorer + "/tx/0x" + hex.EncodeToString(h) }
	var out []ChainEvent
	scan := func(q string, row func(*sql.Rows) (ChainEvent, bool)) {
		rows, err := db.QueryContext(ctx, q, chainFeedPerKind)
		if err != nil {
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			if e, ok := row(rows); ok {
				out = append(out, e)
			}
		}
	}
	usdt := func(micro int64) string { return strconv.FormatFloat(float64(micro)/1e6, 'f', -1, 64) + " USDT" }

	scan(`SELECT tx_hash, amount_micro, seen_at FROM chain_deposits ORDER BY seen_at DESC LIMIT $1`, func(r *sql.Rows) (ChainEvent, bool) {
		var h []byte
		var amt int64
		var at time.Time
		if r.Scan(&h, &amt, &at) != nil {
			return ChainEvent{}, false
		}
		return ChainEvent{At: at, Chain: "whitechain", Kind: "deposit", Title: "Owner deposit", Detail: usdt(amt) + " into escrow", TxURL: wtx(h)}, true
	})
	scan(`SELECT tx_hash, amount_micro, seen_at FROM chain_refunds ORDER BY seen_at DESC LIMIT $1`, func(r *sql.Rows) (ChainEvent, bool) {
		var h []byte
		var amt int64
		var at time.Time
		if r.Scan(&h, &amt, &at) != nil {
			return ChainEvent{}, false
		}
		return ChainEvent{At: at, Chain: "whitechain", Kind: "refund", Title: "Owner refund", Detail: usdt(amt) + " taken back without the coordinator", TxURL: wtx(h)}, true
	})
	scan(`SELECT period, total_micro, post_tx, claim_tx, created_at, updated_at FROM chain_periods
	      WHERE post_tx IS NOT NULL ORDER BY period DESC LIMIT $1`, func(r *sql.Rows) (ChainEvent, bool) {
		var period, total int64
		var post, claim []byte
		var created, updated time.Time
		if r.Scan(&period, &total, &post, &claim, &created, &updated) != nil {
			return ChainEvent{}, false
		}
		if len(claim) > 0 {
			out = append(out, ChainEvent{At: updated, Chain: "whitechain", Kind: "claim", Title: "Providers paid", Detail: "Period " + strconv.FormatInt(period, 10) + " claimed", TxURL: wtx(claim)})
		}
		return ChainEvent{At: created, Chain: "whitechain", Kind: "period", Title: "Payout period posted", Detail: "Period " + strconv.FormatInt(period, 10) + ": " + usdt(total) + " to providers", TxURL: wtx(post)}, true
	})
	scan(`SELECT epoch, leaf_count, anchor_tx, updated_at FROM chain_anchors
	      WHERE status = 'ANCHORED' AND anchor_tx IS NOT NULL ORDER BY epoch DESC LIMIT $1`, func(r *sql.Rows) (ChainEvent, bool) {
		var epoch, leaves int64
		var h []byte
		var at time.Time
		if r.Scan(&epoch, &leaves, &h, &at) != nil {
			return ChainEvent{}, false
		}
		return ChainEvent{At: at, Chain: "whitechain", Kind: "anchor", Title: "Audit root anchored", Detail: "Epoch " + strconv.FormatInt(epoch, 10) + ": " + strconv.FormatInt(leaves, 10) + " audit receipts in one Merkle root", TxURL: wtx(h)}, true
	})
	scan(`SELECT kind, item_count, tx_hash, updated_at FROM arkiv_publications
	      WHERE status = 'CONFIRMED' AND tx_hash IS NOT NULL ORDER BY updated_at DESC LIMIT $1`, func(r *sql.Rows) (ChainEvent, bool) {
		var kind string
		var items sql.NullInt64
		var h []byte
		var at time.Time
		if r.Scan(&kind, &items, &h, &at) != nil {
			return ChainEvent{}, false
		}
		e := ChainEvent{At: at, Chain: "arkiv", Kind: kind, TxURL: atx(h)}
		switch kind {
		case "audit_batch":
			e.Title = "Audit results published"
			if items.Valid {
				e.Detail = strconv.FormatInt(items.Int64, 10) + " audit results"
			}
		case "file_manifest":
			e.Title, e.Detail = "File manifest published", "A stored file's public fingerprint"
		case "file_tombstone":
			e.Title, e.Detail = "File tombstone published", "A deleted file, marked gone"
		default:
			e.Title = kind
		}
		return e, true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > chainFeedMax {
		out = out[:chainFeedMax]
	}
	return out
}

func (s *server) deskChain(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(w, r) {
		return
	}
	events := s.cfg.Chain.chainEvents(r.Context(), s.cfg.DB)
	if events == nil {
		events = []ChainEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"chains": s.cfg.Chain.sections(), "events": events})
}
