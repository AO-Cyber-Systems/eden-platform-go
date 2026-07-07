// repository_aocore_rest_stub.go -- TRD 140-11, the load-bearing half of the proof.
//
// aocore has NO protobuf/Connect. It is REST/OpenAPI, ORG-scoped, edge-signed:
// the org is an APIKey OrganizationID derived from the AOEdge identity-context
// (the verified AOID identity), NEVER from a request body. This stub models that
// shape behind the SAME Repository interface as the eden-biz Connect stub.
//
// NO LIVE CALL. The stub spins up an in-process httptest server that replays the
// committed fixtures/cassettes/aocore_org.json cassette, and makes REAL HTTP
// requests to that loopback server. So the REST transport is genuinely exercised
// (request built, response parsed) while staying fully offline. A request the
// cassette did not record fails loudly rather than silently falling through to a
// real aocore host.
//
// ORG SCOPE IS ENFORCED AT THE CHOKEPOINT, BEFORE THE HTTP CALL. authorizeScope
// runs first; a cross-org (or forged) ScopedContext is denied with ErrScopeDenied
// and the cassette server is never even hit -- identical denial whether the
// other org's entity exists or not (no oracle, no cross-org data served).
package experience

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// aocoreOrgCassetteJSON is the committed, recorded org-scoped aocore exchange.
// Embedded so replay is working-directory independent and provably offline.
//
//go:embed fixtures/cassettes/aocore_org.json
var aocoreOrgCassetteJSON []byte

// aocoreCassette is the on-disk shape of the multi-route org cassette.
type aocoreCassette struct {
	Description string `json:"description"`
	OrgID       string `json:"org_id"`
	Routes      []struct {
		Operation string          `json:"operation"`
		Method    string          `json:"method"`
		Path      string          `json:"path"`
		Status    int             `json:"status"`
		Body      json.RawMessage `json:"body"`
	} `json:"routes"`
}

// aocoreEntityJSON is the org-scoped entity shape the aocore REST endpoints
// return. org_id is part of the body so a leaked cross-org row is structurally
// visible; the stub still re-stamps Entity.ScopeID from the authorized context.
type aocoreEntityJSON struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
}

type aocoreListJSON struct {
	Items      []aocoreEntityJSON `json:"items"`
	NextCursor string             `json:"next_cursor"`
}

// aocoreRestStubRepository is the ORG-scoped REST stub. It owns an httptest
// server replaying the cassette and an http.Client pointed at it. The org id it
// is bound to comes from the cassette (the recorded org); every call is
// authorized against the ONE identity's org grants before any HTTP request.
type aocoreRestStubRepository struct {
	binding  *experiencev1.ServiceTransportBinding
	identity AoidIdentity
	cassette aocoreCassette
	server   *httptest.Server
	client   *http.Client
	baseURL  string
}

func newAocoreRestStubRepository(b *experiencev1.ServiceTransportBinding, identity AoidIdentity) (*aocoreRestStubRepository, error) {
	var c aocoreCassette
	if err := json.Unmarshal(aocoreOrgCassetteJSON, &c); err != nil {
		return nil, fmt.Errorf("aocore-REST stub: decode cassette: %w", err)
	}

	// Replay server: matches the recorded method+path exactly. An unrecorded
	// request returns 501 so a drifting caller can't silently reach a real host.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		for _, route := range c.Routes {
			if req.Method == route.Method && req.URL.Path == route.Path {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(route.Status)
				if len(route.Body) > 0 && string(route.Body) != "null" {
					_, _ = w.Write(route.Body)
				}
				return
			}
		}
		http.Error(w, "no cassette route for "+req.Method+" "+req.URL.Path, http.StatusNotImplemented)
	}))

	return &aocoreRestStubRepository{
		binding:  b,
		identity: identity,
		cassette: c,
		server:   srv,
		client:   srv.Client(),
		baseURL:  srv.URL,
	}, nil
}

func (r *aocoreRestStubRepository) Transport() experiencev1.TransportKind {
	return experiencev1.TransportKind_TRANSPORT_KIND_REST_OPENAPI
}

func (r *aocoreRestStubRepository) Authority() experiencev1.ScopeAuthority {
	return experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG
}

// BaseURL exposes the loopback httptest URL so tests can prove no live call.
func (r *aocoreRestStubRepository) BaseURL() string { return r.baseURL }

// authorize is the ORG-scoped chokepoint. It runs BEFORE any HTTP request, so a
// cross-org/forged context never reaches the cassette server.
func (r *aocoreRestStubRepository) authorize(sc ScopedContext) error {
	return authorizeScope(r.identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_ORG, sc)
}

// do issues a real (loopback) HTTP request to the cassette server. The org id is
// carried in the PATH (edge-signed org scope), modeling aocore's
// /v1/orgs/{org}/... contract -- authority from the verified identity, never the
// body. Returns the raw response body for the caller to decode.
func (r *aocoreRestStubRepository) do(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("aocore-REST stub: marshal payload: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("aocore-REST stub: build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("aocore-REST stub: do request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("aocore-REST stub: read response: %w", err)
	}
	return raw, resp.StatusCode, nil
}

// orgPath builds the org-scoped REST path. The org id is the authorized scope,
// proving ScopeAuthority.ORG is carried structurally in the request.
func orgPath(orgID, suffix string) string {
	return "/v1/orgs/" + orgID + "/tenants" + suffix
}

func (r *aocoreRestStubRepository) Get(ctx context.Context, sc ScopedContext, id string) (Entity, error) {
	if err := r.authorize(sc); err != nil {
		return Entity{}, err
	}
	raw, status, err := r.do(ctx, http.MethodGet, orgPath(sc.ScopeID, "/"+id), nil)
	if err != nil {
		return Entity{}, err
	}
	if status != http.StatusOK {
		return Entity{}, fmt.Errorf("aocore-REST stub: Get status %d", status)
	}
	var ej aocoreEntityJSON
	if err := json.Unmarshal(raw, &ej); err != nil {
		return Entity{}, fmt.Errorf("aocore-REST stub: decode Get: %w", err)
	}
	return entityFromAocore(sc.ScopeID, ej), nil
}

func (r *aocoreRestStubRepository) List(ctx context.Context, sc ScopedContext, page PageRequest) (Page, error) {
	if err := r.authorize(sc); err != nil {
		return Page{}, err
	}
	raw, status, err := r.do(ctx, http.MethodGet, orgPath(sc.ScopeID, ""), nil)
	if err != nil {
		return Page{}, err
	}
	if status != http.StatusOK {
		return Page{}, fmt.Errorf("aocore-REST stub: List status %d", status)
	}
	var lj aocoreListJSON
	if err := json.Unmarshal(raw, &lj); err != nil {
		return Page{}, fmt.Errorf("aocore-REST stub: decode List: %w", err)
	}
	items := make([]Entity, 0, len(lj.Items))
	for _, ej := range lj.Items {
		items = append(items, entityFromAocore(sc.ScopeID, ej))
	}
	if page.Limit > 0 && page.Limit < len(items) {
		items = items[:page.Limit]
	}
	// next_cursor threads through the REST transport -> the surface's pagination.
	return Page{Items: items, NextCursor: lj.NextCursor}, nil
}

func (r *aocoreRestStubRepository) Create(ctx context.Context, sc ScopedContext, e Entity) (Entity, error) {
	if err := r.authorize(sc); err != nil {
		return Entity{}, err
	}
	// Authority is NOT in the body -- only the entity's neutral fields are sent.
	raw, status, err := r.do(ctx, http.MethodPost, orgPath(sc.ScopeID, ""), map[string]any{"name": e.Fields["name"]})
	if err != nil {
		return Entity{}, err
	}
	if status != http.StatusCreated {
		return Entity{}, fmt.Errorf("aocore-REST stub: Create status %d", status)
	}
	var ej aocoreEntityJSON
	if err := json.Unmarshal(raw, &ej); err != nil {
		return Entity{}, fmt.Errorf("aocore-REST stub: decode Create: %w", err)
	}
	return entityFromAocore(sc.ScopeID, ej), nil
}

func (r *aocoreRestStubRepository) Update(ctx context.Context, sc ScopedContext, e Entity) (Entity, error) {
	if err := r.authorize(sc); err != nil {
		return Entity{}, err
	}
	raw, status, err := r.do(ctx, http.MethodPut, orgPath(sc.ScopeID, "/"+e.ID), map[string]any{"name": e.Fields["name"]})
	if err != nil {
		return Entity{}, err
	}
	if status != http.StatusOK {
		return Entity{}, fmt.Errorf("aocore-REST stub: Update status %d", status)
	}
	var ej aocoreEntityJSON
	if err := json.Unmarshal(raw, &ej); err != nil {
		return Entity{}, fmt.Errorf("aocore-REST stub: decode Update: %w", err)
	}
	return entityFromAocore(sc.ScopeID, ej), nil
}

func (r *aocoreRestStubRepository) Delete(ctx context.Context, sc ScopedContext, id string) error {
	if err := r.authorize(sc); err != nil {
		return err
	}
	_, status, err := r.do(ctx, http.MethodDelete, orgPath(sc.ScopeID, "/"+id), nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("aocore-REST stub: Delete status %d", status)
	}
	return nil
}

// entityFromAocore maps the aocore REST JSON to the neutral Entity. ScopeID is
// re-stamped from the AUTHORIZED context (not the body's org_id) so a backend
// that ever returned a foreign org_id cannot smuggle a cross-org row through.
func entityFromAocore(authorizedScope string, ej aocoreEntityJSON) Entity {
	return Entity{
		ID:      ej.ID,
		ScopeID: authorizedScope,
		Fields:  map[string]string{"name": ej.Name, "kind": ej.Kind, "body_org_id": ej.OrgID},
	}
}
