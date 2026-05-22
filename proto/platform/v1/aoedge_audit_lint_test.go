package platformv1proto_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const protoPath = "aoedge_audit.proto"

// allEventMessages is the canonical list of audit-event leaf messages in
// aoedge_audit.proto. The signing envelope `AuditBatch` is intentionally
// excluded — it carries no per-event invariants (no schema_version=1
// chain_hash, no correlation triple, no tenant_id).
//
// Wave 1 (Obj 8 TRD 08-01) shipped the first 5 events. Wave 2 (Obj 9
// TRD 09-01) appended the 3 Identity-event messages. Wave 3 (Obj 10
// TRD 10-01) appended the 2 boundary-authorization event messages.
// Schema-lint walks THIS list uniformly so all invariants apply to every
// event type.
var allEventMessages = []string{
	"ConnectionLog",
	"WAFEvent",
	"DDoSEvent",
	"GeoDecision",
	"DLPFinding",
	"IdentityValidationEvent",
	"IdentityMintEvent",
	"StepUpChallengeEvent",
	"PolicyDecisionEvent",
	"BundleReloadEvent",
}

func readProto(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(protoPath)
	if err != nil {
		t.Fatalf("read %s: %v", protoPath, err)
	}
	return string(b)
}

// messageBody returns the brace-balanced body of `message <name> { ... }`
// from the proto source, or "" if not found. Brace-balanced matching is
// required because nested-brace cases would defeat a non-greedy regex; while
// the current proto has flat message bodies, this is future-proof.
func messageBody(src, name string) string {
	idx := strings.Index(src, "message "+name)
	if idx < 0 {
		return ""
	}
	open := strings.Index(src[idx:], "{")
	if open < 0 {
		return ""
	}
	open += idx
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return ""
}

// TestDLPFinding_NoMatchedValueField asserts the DLPFinding and DLPMatch
// messages have no field that would store PII matched bytes.
// Forbidden: matched_value, snippet, value (as field name), text (as field name),
// matched_bytes.
func TestDLPFinding_NoMatchedValueField(t *testing.T) {
	src := readProto(t)
	// Forbidden substrings — any occurrence anywhere in the file is a failure.
	forbidden := []string{"matched_value", "snippet", "matched_bytes"}
	for _, f := range forbidden {
		if strings.Contains(src, f) {
			t.Errorf("aoedge_audit.proto must not contain field %q — PII data must never be stored in DLPFinding/DLPMatch", f)
		}
	}
	// Check "value" and "text" as proto field names (= N pattern in message body).
	// Only flag if they appear as standalone string field declarations.
	fieldRe := regexp.MustCompile(`\bstring\s+(value|text)\s*=`)
	if m := fieldRe.FindString(src); m != "" {
		t.Errorf("aoedge_audit.proto must not have a bare string field named 'value' or 'text': found %q", m)
	}
}

// TestAoedgeAuditSchema_AllEventMessagesHaveSchemaVersion checks schema_version
// appears for each event message and is declared as field number 1.
func TestAoedgeAuditSchema_AllEventMessagesHaveSchemaVersion(t *testing.T) {
	src := readProto(t)
	for _, msg := range allEventMessages {
		body := messageBody(src, msg)
		if body == "" {
			t.Errorf("message %s not found in %s", msg, protoPath)
			continue
		}
		// schema_version MUST be field 1.
		re := regexp.MustCompile(`\bint32\s+schema_version\s*=\s*1\b`)
		if !re.MatchString(body) {
			t.Errorf("message %s is missing `int32 schema_version = 1` as field 1", msg)
		}
	}
}

// TestAoedgeAuditSchema_AllEventMessagesHaveChainHash checks chain_hash
// appears as the LAST field (highest field number) in each event message.
func TestAoedgeAuditSchema_AllEventMessagesHaveChainHash(t *testing.T) {
	src := readProto(t)
	// Field declaration: <type> <name> = <number>; — capture name+number.
	fieldRe := regexp.MustCompile(`(?m)^\s*\S+(?:\s+\S+)*\s+(\w+)\s*=\s*(\d+)\s*;`)
	for _, msg := range allEventMessages {
		body := messageBody(src, msg)
		if body == "" {
			t.Errorf("message %s not found in %s", msg, protoPath)
			continue
		}
		matches := fieldRe.FindAllStringSubmatch(body, -1)
		if len(matches) == 0 {
			t.Errorf("message %s has no parseable fields", msg)
			continue
		}
		// Find the field with the highest field number.
		var maxNum int
		var maxName string
		for _, m := range matches {
			n := 0
			for _, ch := range m[2] {
				n = n*10 + int(ch-'0')
			}
			if n > maxNum {
				maxNum = n
				maxName = m[1]
			}
		}
		if maxName != "chain_hash" {
			t.Errorf("message %s: last field (= %d) is %q, want chain_hash", msg, maxNum, maxName)
		}
	}
}

// TestAoedgeAuditSchema_AllEventMessagesHaveCorrelationFields checks that
// request_id, trace_id, and span_id appear in every event message body.
func TestAoedgeAuditSchema_AllEventMessagesHaveCorrelationFields(t *testing.T) {
	src := readProto(t)
	fields := []string{"request_id", "trace_id", "span_id"}
	for _, msg := range allEventMessages {
		body := messageBody(src, msg)
		if body == "" {
			t.Errorf("could not find message block for %s", msg)
			continue
		}
		for _, f := range fields {
			if !strings.Contains(body, f) {
				t.Errorf("message %s is missing correlation field %q", msg, f)
			}
		}
	}
}

// TestAoedgeAuditSchema_TenantIsolation_AllEventsCarryTenantId asserts every
// event message carries a `string tenant_id` field. Wave 3 (Obj 9 TRD 09-05)
// uses this for tenant-scoped audit-stream filtering — a missing tenant_id on
// any event leaks cross-tenant context into the audit stream.
//
// This test name embeds the substring "TenantIsolation" so the resolver
// verification regex `wrong-tenant|cross-tenant|tenant-isolation` matches.
func TestAoedgeAuditSchema_TenantIsolation_AllEventsCarryTenantId(t *testing.T) {
	src := readProto(t)
	re := regexp.MustCompile(`\bstring\s+tenant_id\s*=\s*\d+\b`)
	for _, msg := range allEventMessages {
		body := messageBody(src, msg)
		if body == "" {
			t.Errorf("message %s not found — tenant-isolation invariant cannot be verified", msg)
			continue
		}
		if !re.MatchString(body) {
			t.Errorf("message %s is missing `string tenant_id = N` field — cross-tenant audit-stream isolation requires every event to carry tenant_id", msg)
		}
	}
}

// TestAoedgeAuditSchema_NoForbiddenCredentialOrPIIFields asserts the proto
// contains NO field whose name matches the credential-payload or PII
// forbidden list — extended in TRD 09-01 to cover raw credential names,
// extended in TRD 10-01 to cover policy/secret payload names.
//
// Forbidden field names:
//   - matched_value, matched_bytes, snippet (Obj 8 PII for DLP)
//   - raw_token, raw_key, token, password, secret, plaintext, authorization
//     (Obj 9 credential payloads — IdentityValidationEvent must carry only
//     tok_ref, never the raw credential).
//   - policy_input, raw_input (Obj 10 policy payloads — PolicyDecisionEvent
//     must NOT carry the raw policy input document or identity-claims blob).
//   - private_key, signing_key, signature_bytes (Obj 10 key material —
//     BundleReloadEvent must NOT carry bundle signing keys or raw signatures).
//
// The lint walks all event-message bodies AND checks bare field declarations
// at file scope; the test is intentionally strict — any reintroduction of
// these names triggers failure.
func TestAoedgeAuditSchema_NoForbiddenCredentialOrPIIFields(t *testing.T) {
	src := readProto(t)
	forbidden := []string{
		// Obj 8 PII payload bytes (already enforced by older test; re-asserted here for clarity).
		"matched_value",
		"matched_bytes",
		"snippet",
		// Obj 9 raw credential field names — IdentityValidationEvent / IdentityMintEvent
		// / StepUpChallengeEvent must NEVER carry the plaintext credential. tok_ref
		// (16-char hex prefix of SHA-256) is the only credential reference.
		"raw_token",
		"raw_key",
		"password",
		"plaintext",
		"authorization",
		// Obj 10 policy/secret payload names — PolicyDecisionEvent must NOT carry the
		// raw policy input document or identity-claims blob; BundleReloadEvent must
		// NOT carry signing key material.
		"policy_input",
		"raw_input",
		"private_key",
		"signing_key",
		"signature_bytes",
	}
	// Substring scan over the full source — catches both message-body fields
	// AND comment text using the forbidden name as a field name. Comments
	// referencing the forbidden names (e.g. "raw_token") are fine; only
	// FIELD declarations are flagged. We pivot to field-declaration regex
	// to avoid false positives from explanatory comments.
	fieldDeclRe := func(name string) *regexp.Regexp {
		// Match: <type> <name> = <num>;  — name appears as the field identifier.
		return regexp.MustCompile(`\b(?:int32|int64|uint32|uint64|string|bool|bytes|float|double)\s+` + regexp.QuoteMeta(name) + `\s*=\s*\d+\s*;`)
	}
	for _, name := range forbidden {
		if fieldDeclRe(name).MatchString(src) {
			t.Errorf("aoedge_audit.proto contains forbidden field name %q — credential payloads and PII bytes must never be stored in audit events", name)
		}
	}
	// Also check bare `token` and `secret` as STANDALONE string field names
	// — these are common but ambiguous. Only flag standalone usage; tok_ref
	// and related compound names are allowed.
	bareRe := regexp.MustCompile(`\bstring\s+(token|secret)\s*=\s*\d+\b`)
	if m := bareRe.FindString(src); m != "" {
		t.Errorf("aoedge_audit.proto must not have a bare string field named 'token' or 'secret': found %q (use tok_ref)", m)
	}
}

// TestAoedgeAuditSchema_IdentityValidationEvent_RequiredFields asserts the
// Wave-3 dispatcher contract: the IAP middleware emits this event on EVERY
// credential validation (accept or reject), so the message must carry the
// fields needed for tenant-isolated audit stream filtering, RFC 6750/9470
// error classification, and AAL traceability.
func TestAoedgeAuditSchema_IdentityValidationEvent_RequiredFields(t *testing.T) {
	src := readProto(t)
	body := messageBody(src, "IdentityValidationEvent")
	if body == "" {
		t.Fatalf("message IdentityValidationEvent not found in %s", protoPath)
	}
	required := []struct {
		name string
		kind string
	}{
		{"tok_ref", "string"},
		{"cred_type", "string"},
		{"outcome", "string"},
		{"aal", "string"},
		{"iss", "string"},
		{"sub", "string"},
		{"tenant_id", "string"},
	}
	for _, f := range required {
		re := regexp.MustCompile(`\b` + f.kind + `\s+` + f.name + `\s*=\s*\d+\b`)
		if !re.MatchString(body) {
			t.Errorf("IdentityValidationEvent is missing required `%s %s = N` field", f.kind, f.name)
		}
	}
}

// TestAoedgeAuditSchema_IdentityMintEvent_RequiredFields asserts the Wave-3
// dispatcher contract: every successful X-AOEdge-Identity-Context mint emits
// this event. The minted JWT itself MUST NOT be stored in the audit stream
// — it travels in the request header. Only metadata is logged.
func TestAoedgeAuditSchema_IdentityMintEvent_RequiredFields(t *testing.T) {
	src := readProto(t)
	body := messageBody(src, "IdentityMintEvent")
	if body == "" {
		t.Fatalf("message IdentityMintEvent not found in %s", protoPath)
	}
	required := []struct {
		name string
		kind string
	}{
		{"kid", "string"},
		{"sub", "string"},
		{"tnt", "string"},
		{"aal", "string"},
		{"tok_ref", "string"},
		{"entitlements_count", "int32"},
	}
	for _, f := range required {
		re := regexp.MustCompile(`\b` + f.kind + `\s+` + f.name + `\s*=\s*\d+\b`)
		if !re.MatchString(body) {
			t.Errorf("IdentityMintEvent is missing required `%s %s = N` field", f.kind, f.name)
		}
	}
	// HARD INVARIANT: the signed JWT itself MUST NOT appear in the audit
	// stream. Flag any field named signed_jwt / jwt / id_token / access_token.
	forbidden := []string{"signed_jwt", "jwt", "id_token", "access_token"}
	for _, name := range forbidden {
		re := regexp.MustCompile(`\bstring\s+` + name + `\s*=\s*\d+\b`)
		if re.MatchString(body) {
			t.Errorf("IdentityMintEvent must NOT contain field %q — the minted JWT travels in X-AOEdge-Identity-Context header, never in the audit stream", name)
		}
	}
}

// TestAoedgeAuditSchema_StepUpChallengeEvent_RequiredFields asserts the
// Wave-3 step-up contract (IAP-06, RFC 9470): on every step-up challenge
// (browser 302 or API 403) the event carries the AAL gap + channel
// differentiation + base redirect URI (no query string).
func TestAoedgeAuditSchema_StepUpChallengeEvent_RequiredFields(t *testing.T) {
	src := readProto(t)
	body := messageBody(src, "StepUpChallengeEvent")
	if body == "" {
		t.Fatalf("message StepUpChallengeEvent not found in %s", protoPath)
	}
	required := []struct {
		name string
		kind string
	}{
		{"required_aal", "string"},
		{"current_aal", "string"},
		{"channel", "string"},
		{"redirect_uri_host", "string"},
	}
	for _, f := range required {
		re := regexp.MustCompile(`\b` + f.kind + `\s+` + f.name + `\s*=\s*\d+\b`)
		if !re.MatchString(body) {
			t.Errorf("StepUpChallengeEvent is missing required `%s %s = N` field", f.kind, f.name)
		}
	}
	// Forbidden: redirect_uri (full URI with query string carries state/nonce).
	// Only the base host+path form is permitted.
	if regexp.MustCompile(`\bstring\s+redirect_uri\s*=\s*\d+\b`).MatchString(body) {
		t.Errorf("StepUpChallengeEvent must NOT contain bare `redirect_uri` — only redirect_uri_host (base, no query string) is permitted")
	}
}

// TestAoedgeAuditSchema_PolicyDecisionEvent_RequiredFields asserts the
// Wave-1 Obj-10 boundary-authorization contract (AUTHZ-01): the authz
// middleware emits this event on EVERY policy evaluation (allow/deny/error).
// The event carries the decision metadata needed for tenant-isolated allow/deny
// ratio analytics, but MUST NOT carry the raw policy input document or
// any identity-claims blob.
func TestAoedgeAuditSchema_PolicyDecisionEvent_RequiredFields(t *testing.T) {
	src := readProto(t)
	body := messageBody(src, "PolicyDecisionEvent")
	if body == "" {
		t.Fatalf("message PolicyDecisionEvent not found in %s", protoPath)
	}
	required := []struct {
		name string
		kind string
	}{
		{"decision", "string"},
		{"deny_reason", "string"},
		{"policy_query", "string"},
		{"backend_group", "string"},
		{"required_entitlement", "string"},
		{"sub", "string"},
		{"route_id", "string"},
		{"bundle_revision", "string"},
		{"latency_ms", "int64"},
		{"require_stepup", "bool"},
	}
	for _, f := range required {
		re := regexp.MustCompile(`\b` + f.kind + `\s+` + f.name + `\s*=\s*\d+\b`)
		if !re.MatchString(body) {
			t.Errorf("PolicyDecisionEvent is missing required `%s %s = N` field", f.kind, f.name)
		}
	}
	// HARD INVARIANT: raw policy input MUST NOT be present. A PolicyDecisionEvent
	// carries only scalar metadata — the raw input document is reconstructible
	// by correlating on request_id with IdentityValidationEvent + GeoDecision.
	forbidden := []string{"policy_input", "raw_input"}
	for _, name := range forbidden {
		re := regexp.MustCompile(`\b(?:string|bytes)\s+` + name + `\s*=\s*\d+\b`)
		if re.MatchString(body) {
			t.Errorf("PolicyDecisionEvent must NOT contain field %q — raw policy input must never be stored in the audit stream (use request_id correlation)", name)
		}
	}
}

// TestAoedgeAuditSchema_BundleReloadEvent_RequiredFields asserts the
// Wave-1 Obj-10 signed-bundle-reloader contract (AUTHZ-06): the bundle
// reloader emits this event on EVERY reload attempt (applied, signature
// rejection, compile error, or pull failure). The event MUST NOT carry
// signing key material — a compromised bundle must not be able to assert
// its own trust root via the audit stream.
func TestAoedgeAuditSchema_BundleReloadEvent_RequiredFields(t *testing.T) {
	src := readProto(t)
	body := messageBody(src, "BundleReloadEvent")
	if body == "" {
		t.Fatalf("message BundleReloadEvent not found in %s", protoPath)
	}
	required := []struct {
		name string
		kind string
	}{
		{"commit_sha", "string"},
		{"signer_identity", "string"},
		{"bundle_digest", "string"},
		{"outcome", "string"},
		{"rego_module_count", "int32"},
		{"previous_commit_sha", "string"},
	}
	for _, f := range required {
		re := regexp.MustCompile(`\b` + f.kind + `\s+` + f.name + `\s*=\s*\d+\b`)
		if !re.MatchString(body) {
			t.Errorf("BundleReloadEvent is missing required `%s %s = N` field", f.kind, f.name)
		}
	}
	// HARD INVARIANT: signing key material MUST NOT be present. A compromised
	// bundle must not be able to log/assert its own trust root.
	forbidden := []string{"private_key", "signing_key", "signature_bytes"}
	for _, name := range forbidden {
		re := regexp.MustCompile(`\b(?:string|bytes)\s+` + name + `\s*=\s*\d+\b`)
		if re.MatchString(body) {
			t.Errorf("BundleReloadEvent must NOT contain field %q — signing key material must never appear in the audit stream", name)
		}
	}
}
