// repository_connect_stub.go -- TRD 140-11.
//
// The eden-biz side of the two-transport proof: a CONNECT / COMPANY-scoped
// Repository stub. eden-biz is protobuf/Connect and company-scoped; this stub
// models that shape WITHOUT a live Connect call by serving biz-shaped fixtures
// from memory. It implements the SAME Repository interface as the aocore-REST
// stub -- the surface cannot tell them apart.
//
// Scope is enforced at the chokepoint (authorizeScope) BEFORE any data is
// touched, so a cross-company ScopedContext is denied identically whether the
// requested entity exists or not (no oracle).
package experience

import (
	"context"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// connectStubRepository is the in-memory, COMPANY-scoped Connect stub. It holds
// the ONE identity it was bound under so every call re-authorizes the inbound
// ScopedContext against that identity's grants.
type connectStubRepository struct {
	binding  *experiencev1.ServiceTransportBinding
	identity AoidIdentity
}

func newConnectStubRepository(b *experiencev1.ServiceTransportBinding, identity AoidIdentity) *connectStubRepository {
	return &connectStubRepository{binding: b, identity: identity}
}

func (r *connectStubRepository) Transport() experiencev1.TransportKind {
	return experiencev1.TransportKind_TRANSPORT_KIND_CONNECT
}

func (r *connectStubRepository) Authority() experiencev1.ScopeAuthority {
	return experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY
}

// authorize is the COMPANY-scoped chokepoint every method runs through.
func (r *connectStubRepository) authorize(sc ScopedContext) error {
	return authorizeScope(r.identity, experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY, sc)
}

func (r *connectStubRepository) Get(_ context.Context, sc ScopedContext, id string) (Entity, error) {
	if err := r.authorize(sc); err != nil {
		return Entity{}, err
	}
	// Biz-shaped fixture: the entity always belongs to the authorized scope.
	return Entity{
		ID:      id,
		ScopeID: sc.ScopeID,
		Fields:  map[string]string{"name": "Biz Connect Entity", "kind": "connect-company"},
	}, nil
}

func (r *connectStubRepository) List(_ context.Context, sc ScopedContext, page PageRequest) (Page, error) {
	if err := r.authorize(sc); err != nil {
		return Page{}, err
	}
	items := []Entity{
		{ID: "tenant-biz-1", ScopeID: sc.ScopeID, Fields: map[string]string{"name": "Biz One", "kind": "connect-company"}},
		{ID: "tenant-biz-2", ScopeID: sc.ScopeID, Fields: map[string]string{"name": "Biz Two", "kind": "connect-company"}},
	}
	if page.Limit > 0 && page.Limit < len(items) {
		items = items[:page.Limit]
	}
	// A non-empty next cursor proves pagination threads through this transport.
	return Page{Items: items, NextCursor: "connect-cursor-page-2"}, nil
}

func (r *connectStubRepository) Create(_ context.Context, sc ScopedContext, e Entity) (Entity, error) {
	if err := r.authorize(sc); err != nil {
		return Entity{}, err
	}
	created := Entity{ID: "tenant-biz-created", ScopeID: sc.ScopeID, Fields: map[string]string{}}
	for k, v := range e.Fields {
		created.Fields[k] = v
	}
	created.Fields["kind"] = "connect-company"
	return created, nil
}

func (r *connectStubRepository) Update(_ context.Context, sc ScopedContext, e Entity) (Entity, error) {
	if err := r.authorize(sc); err != nil {
		return Entity{}, err
	}
	updated := Entity{ID: e.ID, ScopeID: sc.ScopeID, Fields: map[string]string{}}
	for k, v := range e.Fields {
		updated.Fields[k] = v
	}
	updated.Fields["kind"] = "connect-company"
	return updated, nil
}

func (r *connectStubRepository) Delete(_ context.Context, sc ScopedContext, _ string) error {
	if err := r.authorize(sc); err != nil {
		return err
	}
	return nil
}
