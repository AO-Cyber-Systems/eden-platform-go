package spiffe

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseID — happy path
// ---------------------------------------------------------------------------

func TestParseID_HappyPath(t *testing.T) {
	t.Parallel()

	id, err := ParseID("spiffe://aoid.local/sa/aoid")
	if err != nil {
		t.Fatalf("ParseID returned err=%v, want nil", err)
	}
	if string(id.TrustDomain) != "aoid.local" {
		t.Fatalf("td=%q, want %q", id.TrustDomain, "aoid.local")
	}
	if id.Path != "/sa/aoid" {
		t.Fatalf("path=%q, want %q", id.Path, "/sa/aoid")
	}
}

func TestParseID_PathIsCaseSensitive(t *testing.T) {
	t.Parallel()

	// SPIFFE spec says the path component is case-sensitive — preserve verbatim.
	id, err := ParseID("spiffe://aoid.local/SA/AOID")
	if err != nil {
		t.Fatalf("ParseID returned err=%v, want nil", err)
	}
	if id.Path != "/SA/AOID" {
		t.Fatalf("path=%q, want preserved-case %q", id.Path, "/SA/AOID")
	}
}

// ---------------------------------------------------------------------------
// ParseID — 13 rejection paths
// ---------------------------------------------------------------------------

func TestParseID_RejectsHTTPScheme(t *testing.T) {
	t.Parallel()

	_, err := ParseID("http://aoid.local/sa/aoid")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err=%v, want ErrInvalidID", err)
	}
}

func TestParseID_RejectsHTTPSScheme(t *testing.T) {
	t.Parallel()

	_, err := ParseID("https://aoid.local/sa/aoid")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err=%v, want ErrInvalidID", err)
	}
}

func TestParseID_RejectsFileScheme(t *testing.T) {
	t.Parallel()

	_, err := ParseID("file:///etc/passwd")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err=%v, want ErrInvalidID", err)
	}
}

func TestParseID_RejectsURNScheme(t *testing.T) {
	t.Parallel()

	_, err := ParseID("urn:isbn:0451450523")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err=%v, want ErrInvalidID", err)
	}
}

func TestParseID_RejectsEmptyTrustDomain(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe:///sa/aoid")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestParseID_RejectsUppercaseTrustDomain(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://AOID.LOCAL/sa")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestParseID_RejectsUserinfo(t *testing.T) {
	t.Parallel()

	// Userinfo (user:pass@host) is forbidden by the SPIFFE spec.
	_, err := ParseID("spiffe://user:pass@aoid.local/sa")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err=%v, want ErrInvalidID", err)
	}
}

func TestParseID_RejectsEmptyPath(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsPathWithoutLeadingSlash(t *testing.T) {
	t.Parallel()

	// `spiffe://aoid.local sa` is a malformed URL (space in host).
	_, err := ParseID("spiffe://aoid.local sa")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err=%v, want ErrInvalidID", err)
	}
}

func TestParseID_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local/../etc")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsEmbeddedDotDot(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local/sa/../root")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsQueryString(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local/sa?x=1")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsFragment(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local/sa#frag")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsPathOver2048(t *testing.T) {
	t.Parallel()

	// 2049-char path (leading `/` plus 2048 'a's = 2049 total length).
	path := "/" + strings.Repeat("a", 2048)
	_, err := ParseID("spiffe://aoid.local" + path)
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath (path len=%d)", err, len(path))
	}
}

func TestParseID_RejectsDoubleSlash(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local//sa")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsTrailingSlash(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local/sa/")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsPercentEncodedNull(t *testing.T) {
	t.Parallel()

	_, err := ParseID("spiffe://aoid.local/%00")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidPath", err)
	}
}

func TestParseID_RejectsRawNull(t *testing.T) {
	t.Parallel()

	// Embedded raw NUL byte — url.Parse may accept; we must reject.
	_, err := ParseID("spiffe://aoid.local/\x00")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Either ErrInvalidID (url.Parse may reject) or ErrInvalidPath is acceptable.
	if !errors.Is(err, ErrInvalidID) && !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v, want ErrInvalidID or ErrInvalidPath", err)
	}
}

// ---------------------------------------------------------------------------
// NewTrustDomain
// ---------------------------------------------------------------------------

func TestNewTrustDomain_HappyPath(t *testing.T) {
	t.Parallel()

	cases := []string{
		"aoid.local",
		"aoid-dev.local",
		"aoid.gov.uk",
		"example.com",
		"a",
		"a-b",
		"1.2.3.4", // numeric-only labels are allowed by RFC 5234 host shape
	}
	for _, s := range cases {
		td, err := NewTrustDomain(s)
		if err != nil {
			t.Errorf("NewTrustDomain(%q) err=%v, want nil", s, err)
			continue
		}
		if string(td) != s {
			t.Errorf("NewTrustDomain(%q) returned %q", s, td)
		}
	}
}

func TestNewTrustDomain_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain("")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsUppercase(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain("AOID.local")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsLeadingDot(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain(".aoid.local")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsTrailingDot(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain("aoid.local.")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsLeadingHyphen(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain("-aoid.local")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsTrailingHyphen(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain("aoid-.local")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsDoubleDot(t *testing.T) {
	t.Parallel()

	_, err := NewTrustDomain("aoid..local")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsInvalidChar(t *testing.T) {
	t.Parallel()

	// Underscore is not in RFC 5234 host shape.
	_, err := NewTrustDomain("aoid_local")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}

func TestNewTrustDomain_RejectsOver255Chars(t *testing.T) {
	t.Parallel()

	s := strings.Repeat("a", 256)
	_, err := NewTrustDomain(s)
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain (len=%d)", err, len(s))
	}
}

func TestNewTrustDomain_Accepts255Chars(t *testing.T) {
	t.Parallel()

	// Exactly 255 — valid.
	s := strings.Repeat("a", 255)
	td, err := NewTrustDomain(s)
	if err != nil {
		t.Fatalf("NewTrustDomain(255 chars) err=%v, want nil", err)
	}
	if string(td) != s {
		t.Fatalf("td truncated: len=%d", len(td))
	}
}

// ---------------------------------------------------------------------------
// SPIFFEID.URL() + round-trip
// ---------------------------------------------------------------------------

func TestSPIFFEID_URL_Shape(t *testing.T) {
	t.Parallel()

	id, err := ParseID("spiffe://aoid.local/sa/x")
	if err != nil {
		t.Fatal(err)
	}
	u := id.URL()
	if u == nil {
		t.Fatal("URL() returned nil")
	}
	if u.Scheme != "spiffe" {
		t.Errorf("Scheme=%q, want spiffe", u.Scheme)
	}
	if u.Host != "aoid.local" {
		t.Errorf("Host=%q, want aoid.local", u.Host)
	}
	if u.Path != "/sa/x" {
		t.Errorf("Path=%q, want /sa/x", u.Path)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		t.Errorf("RawQuery=%q Fragment=%q, want both empty", u.RawQuery, u.Fragment)
	}
	if u.User != nil {
		t.Errorf("User=%v, want nil", u.User)
	}
}

func TestSPIFFEID_URLRoundTrip(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"spiffe://aoid.local/sa/aoid",
		"spiffe://example.com/workload/api",
		"spiffe://a/b",
		"spiffe://aoid.gov.uk/services/v1/api",
	}
	for _, in := range inputs {
		id, err := ParseID(in)
		if err != nil {
			t.Errorf("ParseID(%q) err=%v", in, err)
			continue
		}
		round := id.URL().String()
		id2, err := ParseID(round)
		if err != nil {
			t.Errorf("ParseID(round=%q) err=%v", round, err)
			continue
		}
		if id != id2 {
			t.Errorf("round-trip drift: in=%q first=%+v second=%+v", in, id, id2)
		}
	}
}

func TestSPIFFEID_String(t *testing.T) {
	t.Parallel()

	id, err := ParseID("spiffe://aoid.local/sa/x")
	if err != nil {
		t.Fatal(err)
	}
	want := "spiffe://aoid.local/sa/x"
	if got := id.String(); got != want {
		t.Errorf("String()=%q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// BuildSPIFFEID
// ---------------------------------------------------------------------------

func TestBuildSPIFFEID_HappyPath(t *testing.T) {
	t.Parallel()

	td, err := NewTrustDomain("aoid.local")
	if err != nil {
		t.Fatal(err)
	}
	id, err := BuildSPIFFEID(td, "/sa/aoid")
	if err != nil {
		t.Fatal(err)
	}
	if string(id.TrustDomain) != "aoid.local" {
		t.Errorf("td=%q", id.TrustDomain)
	}
	if id.Path != "/sa/aoid" {
		t.Errorf("path=%q", id.Path)
	}
}

func TestBuildSPIFFEID_RejectsInvalidPath(t *testing.T) {
	t.Parallel()

	td, err := NewTrustDomain("aoid.local")
	if err != nil {
		t.Fatal(err)
	}
	invalidPaths := []string{
		"",
		"sa/x",          // no leading slash
		"/sa/",          // trailing slash
		"/sa//x",        // double slash
		"/sa/../etc",    // traversal
		"/%00",          // percent-encoded null
		"/\x00",         // raw null
		"/" + strings.Repeat("a", 2048), // 2049 chars
	}
	for _, p := range invalidPaths {
		_, err := BuildSPIFFEID(td, p)
		if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("BuildSPIFFEID(td, %q) err=%v, want ErrInvalidPath", p, err)
		}
	}
}

func TestBuildSPIFFEID_RejectsInvalidTrustDomain(t *testing.T) {
	t.Parallel()

	// Construct a TrustDomain typed-value that bypassed the validator
	// (e.g., direct cast) — BuildSPIFFEID must re-validate defensively.
	bad := TrustDomain("BAD.LOCAL")
	_, err := BuildSPIFFEID(bad, "/sa/x")
	if !errors.Is(err, ErrInvalidTrustDomain) {
		t.Fatalf("err=%v, want ErrInvalidTrustDomain", err)
	}
}
