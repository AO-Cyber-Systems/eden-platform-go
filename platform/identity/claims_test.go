package identity

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// wireKeys is the complete, frozen set of JSON keys a fully populated Claims
// serialises to. Independent consumers decode this document with their own
// hand-maintained copy of the schema, so the key set is a contract in both
// directions: a key that disappears is a claim those consumers silently read
// as zero, and a key that appears unannounced is a claim they silently drop.
var wireKeys = []string{
	"iss", "sub", "iat", "exp", "jti",
	"tnt", "ent", "aal", "ctx_ver", "tok_ref",
}

// fullyPopulatedClaims returns a context that passes validation against the
// default accepted-version set, with every wire key carrying a value.
func fullyPopulatedClaims() Claims {
	issued := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	return Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://issuer.example/",
			Subject:   "principal-0001",
			IssuedAt:  jwt.NewNumericDate(issued),
			ExpiresAt: jwt.NewNumericDate(issued.Add(2 * time.Minute)),
			ID:        "context-0001",
		},
		Tenant:       "acme",
		Entitlements: []string{"reports.read", "reports.write"},
		AAL:          AAL2,
		CtxVer:       Version,
		TokRef:       "7bcc86737e4b8043",
	}
}

// marshalToMap serialises c and decodes the result into an untyped map, so
// assertions land on the JSON keys actually emitted rather than on the Go
// field names. A struct round trip cannot do this: it passes unchanged even
// if every tag is renamed, which is precisely the break this contract exists
// to prevent.
func marshalToMap(t *testing.T, c Claims) map[string]any {
	t.Helper()

	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal(Claims) returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%s) returned error: %v", raw, err)
	}
	return decoded
}

func TestFrozenTokenType(t *testing.T) {
	// Compared byte-for-byte against the `typ` header by every consumer.
	const want = "identity-context+jwt"
	if JWTType != want {
		t.Errorf("JWTType = %q, want %q (frozen: a change here is rejected at runtime, not at compile time)", JWTType, want)
	}
}

func TestFrozenEmittedVersion(t *testing.T) {
	if Version != 1 {
		t.Errorf("Version = %d, want 1 (emitting outside the version set already whitelisted in the field strands the strictest consumer)", Version)
	}
}

func TestFrozenAssuranceLevels(t *testing.T) {
	levels := []struct {
		got  string
		want string
	}{
		{AAL1, "AAL1"},
		{AAL2, "AAL2"},
		{AAL3, "AAL3"},
	}
	for _, l := range levels {
		if l.got != l.want {
			t.Errorf("assurance level = %q, want %q", l.got, l.want)
		}
	}
}

func TestClaimsMarshalsToExactlyTheFrozenWireKeys(t *testing.T) {
	decoded := marshalToMap(t, fullyPopulatedClaims())

	for _, key := range wireKeys {
		if _, ok := decoded[key]; !ok {
			t.Errorf("wire key %q is missing; consumers decode this key by name and would read the claim as unset", key)
		}
	}

	for key := range decoded {
		if !slices.Contains(wireKeys, key) {
			t.Errorf("unexpected wire key %q; adding a key is a schema change and needs a version bump, not a struct field", key)
		}
	}

	if got, want := len(decoded), len(wireKeys); got != want {
		t.Errorf("emitted %d wire keys, want exactly %d", got, want)
	}
}

func TestClaimsMarshalsValuesUnderTheFrozenKeys(t *testing.T) {
	decoded := marshalToMap(t, fullyPopulatedClaims())

	if got, want := decoded["tnt"], "acme"; got != want {
		t.Errorf("tnt = %v, want %v", got, want)
	}
	if got, want := decoded["sub"], "principal-0001"; got != want {
		t.Errorf("sub = %v, want %v", got, want)
	}
	if got, want := decoded["aal"], "AAL2"; got != want {
		t.Errorf("aal = %v, want %v", got, want)
	}
	// Untyped JSON numbers decode to float64.
	if got, want := decoded["ctx_ver"], float64(1); got != want {
		t.Errorf("ctx_ver = %v, want %v", got, want)
	}
	if got, want := decoded["tok_ref"], "7bcc86737e4b8043"; got != want {
		t.Errorf("tok_ref = %v, want %v", got, want)
	}

	ent, ok := decoded["ent"].([]any)
	if !ok {
		t.Fatalf("ent = %#v, want a JSON array", decoded["ent"])
	}
	if want := []any{"reports.read", "reports.write"}; !slices.Equal(ent, want) {
		t.Errorf("ent = %v, want %v", ent, want)
	}
}

func TestClaimsAlwaysEmitsTheEntitlementsKey(t *testing.T) {
	// `ent` carries no omitempty on purpose: "no entitlements" must be
	// distinguishable on the wire from "this issuer does not speak the claim".
	for name, entitlements := range map[string][]string{
		"nil slice":   nil,
		"empty slice": {},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullyPopulatedClaims()
			c.Entitlements = entitlements

			decoded := marshalToMap(t, c)
			if _, ok := decoded["ent"]; !ok {
				t.Error("ent key is absent; it must be emitted even when there are no entitlements")
			}
		})
	}
}

func TestClaimsUnmarshalsFromTheFrozenWireDocument(t *testing.T) {
	// A hand-written document, not a round trip: this is what arrives from an
	// issuer that mirrors the schema rather than importing it.
	const wire = `{
		"iss": "https://issuer.example/",
		"sub": "principal-0001",
		"iat": 1893553445,
		"exp": 1893553565,
		"jti": "context-0001",
		"tnt": "acme",
		"ent": ["reports.read", "reports.write"],
		"aal": "AAL2",
		"ctx_ver": 1,
		"tok_ref": "7bcc86737e4b8043"
	}`

	var c Claims
	if err := json.Unmarshal([]byte(wire), &c); err != nil {
		t.Fatalf("json.Unmarshal(wire) returned error: %v", err)
	}

	if got, want := c.Issuer, "https://issuer.example/"; got != want {
		t.Errorf("Issuer = %q, want %q", got, want)
	}
	if got, want := c.Subject, "principal-0001"; got != want {
		t.Errorf("Subject = %q, want %q", got, want)
	}
	if got, want := c.ID, "context-0001"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	if got, want := c.Tenant, "acme"; got != want {
		t.Errorf("Tenant = %q, want %q", got, want)
	}
	if want := []string{"reports.read", "reports.write"}; !slices.Equal(c.Entitlements, want) {
		t.Errorf("Entitlements = %v, want %v", c.Entitlements, want)
	}
	if got, want := c.AAL, "AAL2"; got != want {
		t.Errorf("AAL = %q, want %q", got, want)
	}
	if got, want := c.CtxVer, 1; got != want {
		t.Errorf("CtxVer = %d, want %d", got, want)
	}
	if got, want := c.TokRef, "7bcc86737e4b8043"; got != want {
		t.Errorf("TokRef = %q, want %q", got, want)
	}
	if c.IssuedAt == nil || c.IssuedAt.Unix() != 1893553445 {
		t.Errorf("IssuedAt = %v, want unix 1893553445", c.IssuedAt)
	}
	if c.ExpiresAt == nil || c.ExpiresAt.Unix() != 1893553565 {
		t.Errorf("ExpiresAt = %v, want unix 1893553565", c.ExpiresAt)
	}
}

func TestDefaultVersionSetAcceptsOnlyTheEmittedVersion(t *testing.T) {
	set := DefaultVersionSet()

	if !set.Accepts(Version) {
		t.Errorf("DefaultVersionSet() rejects the emitted version %d", Version)
	}
	for _, v := range []int{-1, 0, 2, 3, 99} {
		if set.Accepts(v) {
			t.Errorf("DefaultVersionSet() accepts %d; the default is deliberately narrow and widening must be explicit", v)
		}
	}
}

func TestVersionSetMembership(t *testing.T) {
	tests := []struct {
		name  string
		set   VersionSet
		probe int
		want  bool
	}{
		{"widened set accepts the older version", NewVersionSet(1, 2), 1, true},
		{"widened set accepts the newer version", NewVersionSet(1, 2), 2, true},
		{"widened set rejects outside the set", NewVersionSet(1, 2), 3, false},
		{"single-member set accepts its member", NewVersionSet(2), 2, true},
		{"single-member set rejects a neighbour", NewVersionSet(2), 1, false},
		{"explicitly empty set rejects everything", NewVersionSet(), 1, false},
		{"zero value rejects everything", VersionSet(nil), 1, false},
		{"duplicates collapse", NewVersionSet(1, 1, 1), 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.set.Accepts(tt.probe); got != tt.want {
				t.Errorf("Accepts(%d) = %t, want %t", tt.probe, got, tt.want)
			}
		})
	}
}

func TestDefaultVersionSetReturnsAnIndependentSet(t *testing.T) {
	// Callers widen a set at construction; that must not reach into anyone
	// else's copy.
	first := DefaultVersionSet()
	first[42] = struct{}{}

	if DefaultVersionSet().Accepts(42) {
		t.Error("mutating one DefaultVersionSet() result leaked into the next; the default must not share backing state")
	}
}

func TestVersionSetVersionsIsSortedAndComplete(t *testing.T) {
	got := NewVersionSet(3, 1, 2).Versions()

	if want := []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("Versions() = %v, want %v", got, want)
	}
	if !slices.IsSorted(got) {
		t.Errorf("Versions() = %v, want ascending order", got)
	}
	if got := VersionSet(nil).Versions(); len(got) != 0 {
		t.Errorf("VersionSet(nil).Versions() = %v, want empty", got)
	}
}

func TestClaimsValidate(t *testing.T) {
	accepted := NewVersionSet(1)

	tests := []struct {
		name    string
		mutate  func(*Claims)
		wantErr error // nil means the context must be accepted
	}{
		{
			name:   "fully populated context is accepted",
			mutate: func(*Claims) {},
		},
		{
			name:   "nil entitlements are accepted",
			mutate: func(c *Claims) { c.Entitlements = nil },
		},
		{
			name:   "empty entitlements are accepted",
			mutate: func(c *Claims) { c.Entitlements = []string{} },
		},
		{
			name:    "version outside the accepted set is rejected",
			mutate:  func(c *Claims) { c.CtxVer = 2 },
			wantErr: ErrUnsupportedVersion,
		},
		{
			name:    "unset version is rejected",
			mutate:  func(c *Claims) { c.CtxVer = 0 },
			wantErr: ErrUnsupportedVersion,
		},
		{
			name:    "empty subject is rejected",
			mutate:  func(c *Claims) { c.Subject = "" },
			wantErr: ErrMissingSubject,
		},
		{
			name:    "empty tenant is rejected",
			mutate:  func(c *Claims) { c.Tenant = "" },
			wantErr: ErrMissingTenant,
		},
		{
			name:    "empty assurance level is rejected",
			mutate:  func(c *Claims) { c.AAL = "" },
			wantErr: ErrMissingAssurance,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fullyPopulatedClaims()
			tt.mutate(&c)

			err := c.Validate(accepted)
			switch {
			case tt.wantErr == nil && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Errorf("Validate() = %v, want error matching %v", err, tt.wantErr)
			}
		})
	}
}

func TestClaimsValidateHonoursAWidenedVersionSet(t *testing.T) {
	c := fullyPopulatedClaims()
	c.CtxVer = 2

	if err := c.Validate(NewVersionSet(1, 2)); err != nil {
		t.Errorf("Validate(widened set) = %v, want nil", err)
	}
	if err := c.Validate(NewVersionSet(1)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Validate(narrow set) = %v, want error matching %v", err, ErrUnsupportedVersion)
	}
}

func TestClaimsValidateFailsClosedOnAnEmptyVersionSet(t *testing.T) {
	c := fullyPopulatedClaims()

	if err := c.Validate(VersionSet(nil)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Validate(nil set) = %v, want error matching %v", err, ErrUnsupportedVersion)
	}
	if err := c.Validate(NewVersionSet()); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Validate(empty set) = %v, want error matching %v", err, ErrUnsupportedVersion)
	}
}

func TestClaimsValidateNamesTheRejectedAndAcceptedVersions(t *testing.T) {
	// Version drift is diagnosable only if the rejection says which version
	// arrived and which ones this consumer was configured to take.
	c := fullyPopulatedClaims()
	c.CtxVer = 7

	err := c.Validate(NewVersionSet(1, 2))
	if err == nil {
		t.Fatal("Validate() = nil, want an unsupported-version error")
	}
	for _, want := range []string{"7", "1", "2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error %q does not mention %q", err, want)
		}
	}
}

func TestClaimsValidateDoesNotGateOnTokRef(t *testing.T) {
	// tok_ref is forensic correlation only. A verifier generally cannot
	// recompute it, because it never sees the upstream token.
	c := fullyPopulatedClaims()
	c.TokRef = ""

	if err := c.Validate(DefaultVersionSet()); err != nil {
		t.Errorf("Validate() = %v, want nil; an absent tok_ref must never block authorization", err)
	}
}

var lowerHex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)

func TestTokRefIsSixteenLowercaseHexCharacters(t *testing.T) {
	for _, token := range []string{"a", "upstream-access-token", strings.Repeat("x", 4096)} {
		got := TokRef(token)
		if !lowerHex16.MatchString(got) {
			t.Errorf("TokRef(%.16q...) = %q, want 16 lowercase hex characters", token, got)
		}
	}
}

func TestTokRefMatchesTheFirstEightBytesOfSHA256(t *testing.T) {
	// sha256("upstream-access-token") begins 7bcc86737e4b8043.
	if got, want := TokRef("upstream-access-token"), "7bcc86737e4b8043"; got != want {
		t.Errorf("TokRef() = %q, want %q", got, want)
	}
}

func TestTokRefIsDeterministicAndDistinguishing(t *testing.T) {
	if TokRef("token-a") != TokRef("token-a") {
		t.Error("TokRef() is not deterministic; correlation across log lines depends on it")
	}
	if TokRef("token-a") == TokRef("token-b") {
		t.Error("TokRef() collided on two distinct tokens")
	}
}

func TestTokRefOfAnEmptyTokenIsEmpty(t *testing.T) {
	// Hashing "" would stamp one constant reference onto every context that
	// has no upstream token, which reads like a real correlation handle.
	if got := TokRef(""); got != "" {
		t.Errorf("TokRef(%q) = %q, want %q", "", got, "")
	}
}
