package breach

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"sort"
	"strings"
)

//go:embed testdata/top-passwords-10k.txt
var topPasswordsRaw []byte

// LocalListScreener checks passwords against an embedded list of the
// top common passwords (sourced from xato-net 10-million-passwords-10000,
// lowercased + sort-uniqued at build time).
//
// No network egress — safe for FedRAMP-High deployments and any
// environment that cannot reach api.pwnedpasswords.com.
//
// LocalListScreener is goroutine-safe: the underlying string slice is
// loaded at construction and never mutated.
//
// NOTE: the bundled list is a baseline, not exhaustive. Where policy
// permits egress, combine with HIBPScreener for full corpus coverage.
type LocalListScreener struct {
	// passwords is sorted lexicographically for sort.SearchStrings.
	// All entries are lowercase to permit case-insensitive lookup.
	passwords []string
}

// NewLocalListScreener loads the embedded password list at construction
// time. Defensive sort: the bundled file is pre-sorted at build time,
// but we sort again in case a future contributor disturbs ordering.
func NewLocalListScreener() *LocalListScreener {
	var passwords []string
	scanner := bufio.NewScanner(bytes.NewReader(topPasswordsRaw))
	// Default scanner buffer is plenty for a single line; passwords are short.
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			passwords = append(passwords, line)
		}
	}
	// Defend against accidental file reordering by re-sorting at load time.
	// O(n log n) one-shot at startup; downstream Check is O(log n).
	sort.Strings(passwords)
	return &LocalListScreener{passwords: passwords}
}

// Check implements Screener. Lowercases the input before binary search
// against the embedded (lowercase) corpus. Empty passwords short-circuit
// to (false, 0, nil) — length-floor policy belongs elsewhere.
//
// The ctx parameter is accepted to satisfy the Screener contract; the
// lookup is in-memory and CPU-bound, so cancellation does not block
// I/O. Honoring ctx between every entry would needlessly complicate
// a microsecond-scale binary search.
func (s *LocalListScreener) Check(ctx context.Context, password string) (bool, int, error) {
	if password == "" {
		return false, 0, nil
	}
	needle := strings.ToLower(password)
	idx := sort.SearchStrings(s.passwords, needle)
	if idx < len(s.passwords) && s.passwords[idx] == needle {
		return true, 1, nil
	}
	return false, 0, nil
}
