package composition

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/federation"
)

// BuildFederation constructs a federation.Stack on top of an already-
// built Services. Wires:
//   - In-memory Registry (outbound IdP config)
//   - In-memory SPRegistry (inbound external-IdP config)
//   - SharedKeyResolver from a freshly generated AO ID federation cert
//   - IdPManager + Bridge + StubOIDCExchanger
//
// Phase A (M8): in-memory registries; admin Connect handlers for CRUD
// land in a follow-on. The federation surface is fully operational for
// the runtime flows (metadata, SSO, ACS, callback) — admin tooling
// gates the CONFIGURATION surface, which a future objective covers.
func BuildFederation(cfg *config.Config, svcs *Services) (*federation.Stack, error) {
	if svcs == nil {
		return nil, fmt.Errorf("composition: BuildFederation: nil Services")
	}
	if svcs.Auth == nil || svcs.JWTManager == nil {
		return nil, fmt.Errorf("composition: BuildFederation: Services missing Auth/JWT")
	}

	resolver, err := federation.MustGenerateSharedKey("AO ID Federation")
	if err != nil {
		return nil, fmt.Errorf("composition: federation key: %w", err)
	}

	reg := federation.NewInMemoryRegistry()
	spReg := federation.NewInMemorySPRegistry()
	idpMgr, err := federation.NewIdPManager(reg, resolver)
	if err != nil {
		return nil, fmt.Errorf("composition: federation IdP manager: %w", err)
	}

	bridge, err := federation.NewBridge(svcs.Auth, spReg, svcs.JWTManager, svcs.AuditLogger)
	if err != nil {
		return nil, fmt.Errorf("composition: federation bridge: %w", err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("composition: federation state secret: %w", err)
	}

	return &federation.Stack{
		Registry:    reg,
		SPRegistry:  spReg,
		IdPManager:  idpMgr,
		Bridge:      bridge,
		Exchanger:   &federation.StubOIDCExchanger{},
		BaseURL:     cfg.Issuer,
		StateSecret: secret,
		SessionTTL:  cfg.RefreshTokenExpiry,
	}, nil
}

// Use a non-zero default for the federation surface even when the
// caller doesn't bother. Centralized here so the SessionTTL fallback
// matches the issuer's expectation.
const defaultFederationSessionTTL = 24 * time.Hour
