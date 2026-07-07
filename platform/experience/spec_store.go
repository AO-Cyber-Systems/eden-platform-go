// spec_store.go -- TRD 140-09. The store backing StoreSpec / ResolveSpec.
//
// SCOPE-KEYED BY CONSTRUCTION: a stored AppDefinition is keyed by the OWNING
// principal scope {tenant, org} PLUS its app_def_id. The store NEVER keys by
// app_def_id alone -- a Get that supplies a divergent scope simply misses, so a
// cross-tenant read can never hit another tenant's row (the store itself is a
// tenancy chokepoint, not just the handler). This mirrors the resolve.go
// single-sentinel contract: a miss is indistinguishable from "exists for another
// tenant", denying an existence oracle at the data layer too.
package experience

import (
	"sync"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"google.golang.org/protobuf/proto"
)

// SpecStore persists AppDefinitions under an owning principal scope. Get returns
// (def, true) ONLY when both the scope AND the app_def_id match -- a divergent
// scope misses (ok=false), never returning another scope's definition.
type SpecStore interface {
	// Put stores a deep copy of def under (scope, def.Id). An existing entry at
	// the same key is overwritten (re-store = new version).
	Put(scope PrincipalScope, def *experiencev1.AppDefinition)
	// Get returns a deep copy of the definition stored under (scope, appDefID),
	// or (nil, false) if no entry matches BOTH the scope and the id.
	Get(scope PrincipalScope, appDefID string) (*experiencev1.AppDefinition, bool)
}

// memorySpecStore is an in-memory SpecStore keyed by the composite scope+id. It
// is safe for concurrent use (the httptest server serves requests concurrently).
type memorySpecStore struct {
	mu sync.RWMutex
	// keyed by scopeKey(scope) + "\x00" + appDefID -- the composite owning key.
	defs map[string]*experiencev1.AppDefinition
}

// NewMemorySpecStore returns an empty in-memory SpecStore. The M0 surface uses
// this; a durable store implements the same interface later without a handler
// change.
func NewMemorySpecStore() SpecStore {
	return &memorySpecStore{defs: make(map[string]*experiencev1.AppDefinition)}
}

func (s *memorySpecStore) Put(scope PrincipalScope, def *experiencev1.AppDefinition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep-copy on write so a later caller mutation cannot bleed into the store.
	s.defs[storeKey(scope, def.GetId())] = proto.Clone(def).(*experiencev1.AppDefinition)
}

func (s *memorySpecStore) Get(scope PrincipalScope, appDefID string) (*experiencev1.AppDefinition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	def, ok := s.defs[storeKey(scope, appDefID)]
	if !ok {
		return nil, false
	}
	// Deep-copy on read so the caller cannot mutate the stored row.
	return proto.Clone(def).(*experiencev1.AppDefinition), true
}

// storeKey composes the owning scope + app_def_id into a single map key. The NUL
// separator cannot appear in a tenant/org/id token, so distinct triples never
// collide into one key.
func storeKey(scope PrincipalScope, appDefID string) string {
	return scope.TenantID + "\x00" + scope.OrgID + "\x00" + appDefID
}
