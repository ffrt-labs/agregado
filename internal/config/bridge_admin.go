package config

import (
	"strings"

	"github.com/caarlos0/env/v10"
)

// BridgeAdmin configures the Newsletters app (cmd/bridge-admin, spec #225).
// It is a separate struct because that binary never touches Agregado's
// Postgres or enrichment ingress, so Load()'s required fields don't apply.
//
// Its credentials are dedicated ones, independently revocable from n8n's and
// from Agregado's Workers AI token (ADR-0012).
type BridgeAdmin struct {
	Http
	Logging
	Cloudflare BridgeAdminCloudflare
	Bridge     Bridge
	Miniflux   Miniflux
	Resend     BridgeAdminResend
}

// BridgeAdminResend is the app's own Resend key, for reading received mail
// (Confirm and Unclaimed mail). Resend has no read-only key type: this one
// must be able to read received emails, so it is a full-access key, kept
// apart from n8n's so either can be revoked alone (ADR-0012).
type BridgeAdminResend struct {
	APIKey string `env:"BRIDGE_ADMIN_RESEND_API_KEY,required,notEmpty"`
}

type BridgeAdminCloudflare struct {
	// AccountID is the same account Workers AI uses, hence the shared name.
	AccountID string `env:"CLOUDFLARE_ACCOUNT_ID,required,notEmpty"`
	// APIToken needs D1 Edit on the Bridge's database (and, from #230, R2
	// write on its bucket). Never the Workers AI token.
	APIToken string `env:"BRIDGE_ADMIN_CF_API_TOKEN,required,notEmpty"`
}

// Bridge locates the Bridge's storage and addresses.
type Bridge struct {
	// D1DatabaseID is the `bridge` D1 database (email-worker/wrangler.jsonc).
	// Same name n8n uses.
	D1DatabaseID string `env:"BRIDGE_D1_DATABASE_ID,required,notEmpty"`
	// Origin is the Bridge Worker's scheme and host, no trailing slash (one is
	// trimmed). Same name and meaning as n8n's BRIDGE_ORIGIN. Feeds live at
	// Origin + "/feed/<alias>.atom".
	Origin string `env:"BRIDGE_ORIGIN,required,notEmpty"`
	// ReceivingDomain is the Resend catch-all domain: every Source's address
	// is <alias>@ReceivingDomain.
	ReceivingDomain string `env:"BRIDGE_RECEIVING_DOMAIN,required,notEmpty"`
}

type Miniflux struct {
	// URL is the Miniflux origin, e.g. http://miniflux:8080 over the edge
	// network.
	URL    string `env:"MINIFLUX_URL,required,notEmpty"`
	APIKey string `env:"BRIDGE_ADMIN_MINIFLUX_API_KEY,required,notEmpty"`
}

func LoadBridgeAdmin() (*BridgeAdmin, error) {
	cfg := &BridgeAdmin{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	cfg.Bridge.Origin = strings.TrimRight(cfg.Bridge.Origin, "/")
	return cfg, nil
}
