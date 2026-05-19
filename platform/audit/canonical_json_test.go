package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// canonicalSampleEvent builds an Event populated across all the canonical fields
// so the marshaler is exercised end-to-end. Each test typically mutates a copy
// and asserts a specific property.
func canonicalSampleEvent() Event {
	return Event{
		CompanyID:   "11111111-1111-1111-1111-111111111111",
		ActorID:     "22222222-2222-2222-2222-222222222222",
		ActorKind:   "human",
		SubjectID:   "33333333-3333-3333-3333-333333333333",
		SubjectKind: "account",
		Action:      "identity.account.update",
		Resource:    "account",
		ResourceID:  "44444444-4444-4444-4444-444444444444",
		Decision:    "allow",
		IPAddress:   "10.0.0.1",
		Details: map[string]any{
			"jti":    "01HXYZABCDEFGHJKMNPQRSTVWX",
			"before": map[string]any{"name": "alice", "role": "user"},
			"after":  map[string]any{"role": "admin", "name": "alice"},
		},
		MFA: &MFAAttestation{
			Presented:       []string{"password", "totp"},
			Verified:        []string{"password", "totp"},
			StepUpSatisfied: true,
			AALAchieved:     "AAL2",
		},
		Federation: []FederationLink{
			{IDP: "login.gov", Level: "IAL2/AAL2", TrustLink: "trust-link-1"},
		},
		Risk: &RiskAttestation{
			Score: 12,
			Signals: []RiskSignal{
				{Signal: "new_device", Weight: 5},
				{Signal: "unusual_geo", Weight: 7},
			},
		},
	}
}

func TestMarshalCanonical_StableAcrossRuns(t *testing.T) {
	e := canonicalSampleEvent()
	first, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	for i := 0; i < 99; i++ {
		got, err := MarshalCanonical(e, "aoid", 1700000000)
		require.NoError(t, err)
		require.True(t, bytes.Equal(first, got), "run %d differs", i)
	}
}

func TestMarshalCanonical_MapKeySorting(t *testing.T) {
	e := Event{
		CompanyID: "55555555-5555-5555-5555-555555555555",
		ActorID:   "66666666-6666-6666-6666-666666666666",
		Action:    "test",
		Details: map[string]any{
			"jti": "JTI1",
			"z":   "last",
			"a":   "first",
			"m":   "mid",
		},
	}
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	// Inside the "details" object, keys must be lexicographically sorted.
	// Find the substring after "\"details\":{".
	s := string(out)
	idx := strings.Index(s, "\"details\":{")
	require.GreaterOrEqual(t, idx, 0, "details key missing: %s", s)
	tail := s[idx:]
	// "a" must precede "jti" must precede "m" must precede "z".
	posA := strings.Index(tail, "\"a\":")
	posJTI := strings.Index(tail, "\"jti\":")
	posM := strings.Index(tail, "\"m\":")
	posZ := strings.Index(tail, "\"z\":")
	require.Less(t, posA, posJTI, "a should precede jti in %s", tail)
	require.Less(t, posJTI, posM, "jti should precede m in %s", tail)
	require.Less(t, posM, posZ, "m should precede z in %s", tail)
}

func TestMarshalCanonical_OmitEmptyFields(t *testing.T) {
	e := Event{
		CompanyID: "77777777-7777-7777-7777-777777777777",
		ActorID:   "88888888-8888-8888-8888-888888888888",
		Action:    "minimal.event",
		Details:   map[string]any{"jti": "JTI-MIN"},
	}
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	s := string(out)
	// Must include the populated fields.
	require.Contains(t, s, "\"event_type\":\"minimal.event\"")
	require.Contains(t, s, "\"iss\":\"aoid\"")
	require.Contains(t, s, "\"iat\":1700000000")
	// Must NOT include empty/zero fields.
	require.NotContains(t, s, "\"actor_kind\":")
	require.NotContains(t, s, "\"subject_id\":")
	require.NotContains(t, s, "\"mfa\":")
	require.NotContains(t, s, "\"federation\":")
	require.NotContains(t, s, "\"risk_score\":")
	require.NotContains(t, s, "\"risk_signals\":")
	require.NotContains(t, s, "\"decision\":")
	require.NotContains(t, s, "\"source_ip\":")
}

func TestMarshalCanonical_NestedMapsSort(t *testing.T) {
	e := Event{
		CompanyID: "99999999-9999-9999-9999-999999999999",
		ActorID:   "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		Action:    "nested.test",
		Details: map[string]any{
			"jti": "JTI-N",
			"foo": map[string]any{"z": 1, "a": 2, "m": 3},
		},
	}
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	s := string(out)
	// Inside the nested foo object, keys must also be sorted.
	idx := strings.Index(s, "\"foo\":{")
	require.GreaterOrEqual(t, idx, 0)
	tail := s[idx:]
	posA := strings.Index(tail, "\"a\":2")
	posM := strings.Index(tail, "\"m\":3")
	posZ := strings.Index(tail, "\"z\":1")
	require.Less(t, posA, posM)
	require.Less(t, posM, posZ)
}

func TestMarshalCanonical_NoHTMLEscape(t *testing.T) {
	e := Event{
		CompanyID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		ActorID:   "cccccccc-cccc-cccc-cccc-cccccccccccc",
		Action:    "<script>alert(1)</script>",
		Details:   map[string]any{"jti": "JTI-H"},
	}
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	s := string(out)
	// '<' must NOT be escaped to <.
	require.NotContains(t, s, "\\u003c")
	require.Contains(t, s, "<script>")
}

func TestMarshalCanonical_LowercaseUUIDs(t *testing.T) {
	e := Event{
		CompanyID: "DEADBEEF-DEAD-BEEF-DEAD-BEEFDEADBEEF",
		ActorID:   "ABCD1234-ABCD-1234-ABCD-1234ABCD1234",
		Action:    "case.test",
		Details:   map[string]any{"jti": "JTI-L"},
	}
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	s := string(out)
	require.Contains(t, s, "\"tenant_id\":\"deadbeef-dead-beef-dead-beefdeadbeef\"")
	require.Contains(t, s, "\"actor_id\":\"abcd1234-abcd-1234-abcd-1234abcd1234\"")
	require.NotContains(t, s, "DEADBEEF")
	require.NotContains(t, s, "ABCD1234")
}

func TestRoundTrip_UnmarshalCanonical_Identity(t *testing.T) {
	e := canonicalSampleEvent()
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	roundTrip, iss, iat, err := UnmarshalCanonical(out)
	require.NoError(t, err)
	require.Equal(t, "aoid", iss)
	require.Equal(t, int64(1700000000), iat)
	// Re-marshal and compare bytes for semantic equivalence.
	out2, err := MarshalCanonical(roundTrip, "aoid", 1700000000)
	require.NoError(t, err)
	require.True(t, bytes.Equal(out, out2), "round-trip not identity:\n  first: %s\n  second: %s", out, out2)
}

func TestMarshalCanonical_RiskNilSafe(t *testing.T) {
	e := Event{
		CompanyID: "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee",
		ActorID:   "ffffffff-ffff-ffff-ffff-ffffffffffff",
		Action:    "nil-risk.test",
		Details:   map[string]any{"jti": "JTI-R"},
		Risk:      nil,
	}
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	s := string(out)
	require.NotContains(t, s, "\"risk_score\":")
	require.NotContains(t, s, "\"risk_signals\":")
}

// TestMarshalCanonical_ValidJSON proves output is always parseable.
func TestMarshalCanonical_ValidJSON(t *testing.T) {
	e := canonicalSampleEvent()
	out, err := MarshalCanonical(e, "aoid", 1700000000)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(out, &generic))
	require.Equal(t, "aoid", generic["iss"])
	require.Equal(t, "identity.account.update", generic["event_type"])
}
