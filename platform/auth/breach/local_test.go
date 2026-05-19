// Test list (hand-written — no LLM-generated payloads):
//   - TestLocalListScreener_KnownCommonPassword
//   - TestLocalListScreener_LessCommonPassword
//   - TestLocalListScreener_EmptyPassword
//   - TestLocalListScreener_CaseSensitivity
//   - TestLocalListScreener_LoadedListIsSorted
//   - TestLocalListScreener_LoadedListNonEmpty
package breach

import (
	"context"
	"sort"
	"testing"
)

// TestLocalListScreener_KnownCommonPassword — the bundled list contains
// "password" (xato-net top-10k entry); Check should report compromised=true.
func TestLocalListScreener_KnownCommonPassword(t *testing.T) {
	s := NewLocalListScreener()
	for _, p := range []string{"password", "123456", "qwerty"} {
		compromised, count, err := s.Check(context.Background(), p)
		if err != nil {
			t.Fatalf("Check(%q) returned err: %v", p, err)
		}
		if !compromised {
			t.Fatalf("expected %q to be compromised", p)
		}
		if count != 1 {
			t.Fatalf("expected count=1 for %q, got %d", p, count)
		}
	}
}

// TestLocalListScreener_LessCommonPassword — passphrases not in the
// top-10k corpus must report compromised=false.
func TestLocalListScreener_LessCommonPassword(t *testing.T) {
	s := NewLocalListScreener()
	// "correct horse battery staple" is a hand-picked passphrase known to
	// be absent from xato-net top-10k (confirmed by grep at corpus-build time).
	compromised, count, err := s.Check(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if compromised {
		t.Fatal("expected compromised=false for uncommon passphrase")
	}
	if count != 0 {
		t.Fatalf("expected count=0, got %d", count)
	}
}

// TestLocalListScreener_EmptyPassword — empty input returns (false, 0, nil).
func TestLocalListScreener_EmptyPassword(t *testing.T) {
	s := NewLocalListScreener()
	compromised, count, err := s.Check(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if compromised {
		t.Fatal("expected compromised=false for empty password")
	}
	if count != 0 {
		t.Fatalf("expected count=0, got %d", count)
	}
}

// TestLocalListScreener_CaseSensitivity — lookup MUST be case-insensitive
// since the list is normalized to lowercase. "PASSWORD" should hit.
func TestLocalListScreener_CaseSensitivity(t *testing.T) {
	s := NewLocalListScreener()
	compromised, _, err := s.Check(context.Background(), "PASSWORD")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !compromised {
		t.Fatal("expected compromised=true for uppercase variant")
	}
}

// TestLocalListScreener_LoadedListIsSorted — binary search correctness
// depends on the loaded slice being sorted; this test guards against
// accidental list-file reordering.
func TestLocalListScreener_LoadedListIsSorted(t *testing.T) {
	s := NewLocalListScreener()
	if !sort.StringsAreSorted(s.passwords) {
		t.Fatal("loaded password list is not sorted")
	}
}

// TestLocalListScreener_LoadedListNonEmpty — guard against an empty embed
// (would silently fail-open every Check otherwise).
func TestLocalListScreener_LoadedListNonEmpty(t *testing.T) {
	s := NewLocalListScreener()
	if got := len(s.passwords); got < 9000 {
		t.Fatalf("expected at least 9000 passwords loaded, got %d", got)
	}
}
