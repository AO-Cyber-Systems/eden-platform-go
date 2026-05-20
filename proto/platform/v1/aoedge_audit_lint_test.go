package platformv1proto_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const protoPath = "aoedge_audit.proto"

func readProto(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(protoPath)
	if err != nil {
		t.Fatalf("read %s: %v", protoPath, err)
	}
	return string(b)
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

// TestAoedgeAuditProto_AllEventMessagesHaveSchemaVersion checks schema_version
// appears for each of the five event message types.
func TestAoedgeAuditProto_AllEventMessagesHaveSchemaVersion(t *testing.T) {
	src := readProto(t)
	messages := []string{"ConnectionLog", "WAFEvent", "DDoSEvent", "GeoDecision", "DLPFinding"}
	for _, msg := range messages {
		// Find the message block and confirm schema_version is inside it.
		// (?s) flag treats . as matching newlines; the [^}]* anchor needs DOTALL
		// to span multi-line message bodies.
		re := regexp.MustCompile(`(?s)message\s+` + msg + `\s*\{[^}]*schema_version`)
		if !re.MatchString(src) {
			t.Errorf("message %s is missing schema_version field", msg)
		}
	}
}

// TestAoedgeAuditProto_AllEventMessagesHaveChainHash checks chain_hash appears
// in each of the five event message types.
func TestAoedgeAuditProto_AllEventMessagesHaveChainHash(t *testing.T) {
	src := readProto(t)
	messages := []string{"ConnectionLog", "WAFEvent", "DDoSEvent", "GeoDecision", "DLPFinding"}
	for _, msg := range messages {
		re := regexp.MustCompile(`(?s)message\s+` + msg + `\s*\{[^}]*chain_hash`)
		if !re.MatchString(src) {
			t.Errorf("message %s is missing chain_hash field", msg)
		}
	}
}

// TestAoedgeAuditProto_AllEventMessagesHaveCorrelationFields checks that
// request_id, trace_id, and span_id appear in all five event messages.
func TestAoedgeAuditProto_AllEventMessagesHaveCorrelationFields(t *testing.T) {
	src := readProto(t)
	messages := []string{"ConnectionLog", "WAFEvent", "DDoSEvent", "GeoDecision", "DLPFinding"}
	fields := []string{"request_id", "trace_id", "span_id"}
	for _, msg := range messages {
		re := regexp.MustCompile(`(?s)message\s+` + msg + `\s*\{.*?\}`)
		block := re.FindString(src)
		if block == "" {
			t.Errorf("could not find message block for %s", msg)
			continue
		}
		for _, f := range fields {
			if !strings.Contains(block, f) {
				t.Errorf("message %s is missing correlation field %q", msg, f)
			}
		}
	}
}
