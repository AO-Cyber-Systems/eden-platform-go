package hashutil

import (
	"strings"
	"testing"
)

func TestSHA256Hash(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple string", "hello"},
		{"empty string", ""},
		{"api key format", "sk-abc123def456"},
		{"unicode", "héllo wörld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := SHA256Hash(tt.input)
			if len(hash) != 64 {
				t.Fatalf("SHA256Hash() length = %d, want 64", len(hash))
			}
			if got := SHA256Hash(tt.input); got != hash {
				t.Fatalf("SHA256Hash() not deterministic: %q != %q", got, hash)
			}
		})
	}
}

func TestSHA256Hash_KnownValue(t *testing.T) {
	// Known SHA256 of "hello"
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got := SHA256Hash("hello"); got != want {
		t.Fatalf("SHA256Hash(%q) = %q, want %q", "hello", got, want)
	}
}

func TestSHA256Hash_DifferentInputs(t *testing.T) {
	if SHA256Hash("input-one") == SHA256Hash("input-two") {
		t.Fatal("SHA256Hash() collision on distinct inputs")
	}
}

func TestSHA256HashBytes_MatchesString(t *testing.T) {
	const input = "test data"
	if SHA256Hash(input) != SHA256HashBytes([]byte(input)) {
		t.Fatal("SHA256HashBytes diverges from SHA256Hash for the same payload")
	}
}

func TestSHA256HashBytes_EmptyInput(t *testing.T) {
	if got := SHA256HashBytes([]byte{}); got != SHA256Hash("") {
		t.Fatalf("SHA256HashBytes(empty) = %q, want %q", got, SHA256Hash(""))
	}
}

func TestSHA256HashBytes_LargeInput(t *testing.T) {
	if got := SHA256HashBytes(make([]byte, 1<<20)); len(got) != 64 {
		t.Fatalf("SHA256HashBytes(1MB) length = %d, want 64", len(got))
	}
}

func TestChainHash_Deterministic(t *testing.T) {
	a := ChainHash("prev-hash", "INSERT", "api_keys", "uuid-123", "admin@test.com")
	b := ChainHash("prev-hash", "INSERT", "api_keys", "uuid-123", "admin@test.com")
	if a != b {
		t.Fatalf("ChainHash() not deterministic: %q != %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("ChainHash() length = %d, want 64", len(a))
	}
}

func TestChainHash_FieldChangesMatter(t *testing.T) {
	base := ChainHash("prev", "INSERT", "table", "obj1", "user1")
	if base == ChainHash("prev", "UPDATE", "table", "obj1", "user1") {
		t.Fatal("ChainHash should differ when action changes")
	}
	if base == ChainHash("different-prev", "INSERT", "table", "obj1", "user1") {
		t.Fatal("ChainHash should differ when previousHash changes")
	}
}

func TestChainHash_FieldOrderMatters(t *testing.T) {
	a := ChainHash("a", "b", "c", "d", "e")
	// Swap action and tableName.
	b := ChainHash("a", "c", "b", "d", "e")
	if a == b {
		t.Fatal("ChainHash should differ when field order changes (pipe delimiter expected)")
	}
}

func TestChainHash_LowercaseHex(t *testing.T) {
	got := ChainHash("p", "a", "t", "o", "u")
	if strings.ToLower(got) != got {
		t.Fatalf("ChainHash() = %q; expected lowercase hex", got)
	}
}
