package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// canonicalPayload is the on-the-wire JSON shape the signing/verification path
// agrees on. Producer (SignedStore) and verifier (downstream AOAudit) MUST
// import this package and use these exact JSON tags + types — drift here is a
// security incident, not a bug.
//
// Field set is fixed for v1 of the audit schema. New fields require a schema
// version bump and a parallel verifier-side update.
type canonicalPayload struct {
	EventID     string                     `json:"event_id,omitempty"`
	Issuer      string                     `json:"iss,omitempty"`
	IssuedAt    int64                      `json:"iat,omitempty"`
	JTI         string                     `json:"jti,omitempty"`
	EventType   string                     `json:"event_type,omitempty"`
	TenantID    string                     `json:"tenant_id,omitempty"`
	ActorID     string                     `json:"actor_id,omitempty"`
	ActorKind   string                     `json:"actor_kind,omitempty"`
	SubjectID   string                     `json:"subject_id,omitempty"`
	SubjectKind string                     `json:"subject_kind,omitempty"`
	Resource    string                     `json:"resource,omitempty"`
	ResourceID  string                     `json:"resource_id,omitempty"`
	Decision    string                     `json:"decision,omitempty"`
	SourceIP    string                     `json:"source_ip,omitempty"`
	MFA         *MFAAttestation            `json:"mfa,omitempty"`
	Federation  []FederationLink           `json:"federation,omitempty"`
	RiskScore   int32                      `json:"risk_score,omitempty"`
	RiskSignals []RiskSignal               `json:"risk_signals,omitempty"`
	Details     map[string]json.RawMessage `json:"details,omitempty"`
}

// MarshalCanonical produces a deterministic byte-for-byte stable JSON encoding
// of e. Used as the JWS payload by SignedStore; same encoder is used by the
// downstream verifier (AOAudit) so identical inputs produce identical
// signature-input bytes.
//
// Determinism rules:
//  1. All field keys lowercased (handled by struct tags) + omitempty (nil
//     pointers, empty slices, empty strings, zero ints are dropped).
//  2. Map keys (Details + any nested maps) sorted lexicographically.
//  3. No HTML escaping ('<' is preserved, not encoded as <).
//  4. No whitespace / indentation.
//
// issuer and iatSec are supplied by SignedStore at sign time so canonical_json
// itself stays stateless.
//
// UUID-shaped fields (tenant_id, actor_id, subject_id) are lowercased
// pre-encoding so the verifier can canonicalize without re-parsing UUIDs.
func MarshalCanonical(e Event, issuer string, iatSec int64) ([]byte, error) {
	jti := eventJTI(e)
	cp := canonicalPayload{
		EventID:     jti,
		Issuer:      issuer,
		IssuedAt:    iatSec,
		JTI:         jti,
		EventType:   e.Action,
		TenantID:    strings.ToLower(e.CompanyID),
		ActorID:     strings.ToLower(e.ActorID),
		ActorKind:   e.ActorKind,
		SubjectID:   strings.ToLower(e.SubjectID),
		SubjectKind: e.SubjectKind,
		Resource:    e.Resource,
		ResourceID:  e.ResourceID,
		Decision:    e.Decision,
		SourceIP:    e.IPAddress,
		MFA:         e.MFA,
		Federation:  e.Federation,
	}
	if e.Risk != nil {
		cp.RiskScore = e.Risk.Score
		cp.RiskSignals = e.Risk.Signals
	}
	if len(e.Details) > 0 {
		cp.Details = make(map[string]json.RawMessage, len(e.Details))
		keys := make([]string, 0, len(e.Details))
		for k := range e.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b, err := canonicalizeAny(e.Details[k])
			if err != nil {
				return nil, fmt.Errorf("audit: canonicalize details[%q]: %w", k, err)
			}
			cp.Details[k] = b
		}
	}

	// Marshal with HTML escaping off and no indentation. json.Encoder appends
	// a trailing newline that we strip for byte-stability.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cp); err != nil {
		return nil, fmt.Errorf("audit: encode canonical payload: %w", err)
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")

	// encoding/json does not guarantee deterministic map iteration for
	// map[string]json.RawMessage values during top-level Encode (it sorts
	// string keys for map[string]X — see encoding/json/encode.go). The
	// Details field is map[string]json.RawMessage so it IS sorted by the
	// encoder. We sorted the keys explicitly above as belt-and-suspenders.
	return out, nil
}

// canonicalizeAny re-encodes a JSON value with sorted map keys and the same
// non-HTML-escape policy as MarshalCanonical. Used recursively for the values
// inside Event.Details so nested maps also produce stable bytes.
//
// Strategy: marshal to bytes, unmarshal to interface, then re-marshal via a
// recursive walker. The intermediate Unmarshal collapses any prior key order
// into a map[string]any whose ordering we then control.
func canonicalizeAny(v any) (json.RawMessage, error) {
	// First marshal to a byte view so we can re-decode into a generic shape.
	var src []byte
	switch x := v.(type) {
	case json.RawMessage:
		src = []byte(x)
	default:
		b, err := jsonMarshalNoHTML(x)
		if err != nil {
			return nil, err
		}
		src = b
	}
	var decoded any
	if err := json.Unmarshal(src, &decoded); err != nil {
		return nil, err
	}
	return canonicalizeDecoded(decoded)
}

// canonicalizeDecoded walks a decoded interface tree and emits sorted-key,
// non-HTML-escaped JSON.
func canonicalizeDecoded(v any) (json.RawMessage, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := jsonMarshalNoHTML(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			cb, err := canonicalizeDecoded(x[k])
			if err != nil {
				return nil, err
			}
			buf.Write(cb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, el := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			eb, err := canonicalizeDecoded(el)
			if err != nil {
				return nil, err
			}
			buf.Write(eb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return jsonMarshalNoHTML(x)
	}
}

// jsonMarshalNoHTML marshals v with HTML escaping disabled and trailing
// newline trimmed. encoding/json's package-level Marshal escapes '<', '>',
// '&' — we MUST NOT do that for canonical JSON, because re-canonicalization
// on the verifier side would invert those escapes and break signature input.
func jsonMarshalNoHTML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// UnmarshalCanonical parses a canonical JSON payload back into an Event for
// downstream verifiers. The (iss, iat) fields ride alongside in the canonical
// payload, so we return them separately rather than tucking them onto Event.
//
// The inverse is not byte-perfect because Details values are decoded via
// encoding/json's default rules (numbers become float64, etc.). Verifier code
// that re-canonicalizes via MarshalCanonical on the returned Event is the
// supported path for re-signing/re-verification.
func UnmarshalCanonical(b []byte) (Event, string, int64, error) {
	var cp canonicalPayload
	if err := json.Unmarshal(b, &cp); err != nil {
		return Event{}, "", 0, fmt.Errorf("audit: unmarshal canonical: %w", err)
	}
	e := Event{
		CompanyID:   cp.TenantID,
		ActorID:     cp.ActorID,
		ActorKind:   cp.ActorKind,
		SubjectID:   cp.SubjectID,
		SubjectKind: cp.SubjectKind,
		Action:      cp.EventType,
		Resource:    cp.Resource,
		ResourceID:  cp.ResourceID,
		Decision:    cp.Decision,
		IPAddress:   cp.SourceIP,
		MFA:         cp.MFA,
		Federation:  cp.Federation,
	}
	if cp.RiskScore != 0 || len(cp.RiskSignals) > 0 {
		e.Risk = &RiskAttestation{Score: cp.RiskScore, Signals: cp.RiskSignals}
	}
	if len(cp.Details) > 0 {
		e.Details = make(map[string]any, len(cp.Details))
		for k, raw := range cp.Details {
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				return Event{}, "", 0, fmt.Errorf("audit: unmarshal details[%q]: %w", k, err)
			}
			e.Details[k] = v
		}
		// Ensure the jti round-trips as a string for eventJTI() callers.
		if jtiAny, ok := e.Details["jti"]; ok {
			if s, ok := jtiAny.(string); ok && s != "" {
				e.Details["jti"] = s
			}
		}
	}
	return e, cp.Issuer, cp.IssuedAt, nil
}

// eventJTI returns the stable JWT ID for e. The JTI is the SAME as the
// audit-event identifier — both jti (RFC 7519) and event_id in the canonical
// payload reference the same ULID/UUID string.
//
// Preference order:
//  1. e.Details["jti"] if set and a non-empty string.
//  2. empty string (caller — SignedStore — generates one and stores in
//     Details["jti"] before calling MarshalCanonical).
func eventJTI(e Event) string {
	if e.Details == nil {
		return ""
	}
	if v, ok := e.Details["jti"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
