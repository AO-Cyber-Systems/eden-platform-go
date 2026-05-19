package spiffe

import (
	"errors"
	"net/url"
	"strings"
)

// Sentinel errors returned by ParseID, NewTrustDomain, and BuildSPIFFEID.
// Callers should compare via errors.Is so wrapping by upstream layers
// (e.g. service-layer error translation) remains transparent.
var (
	// ErrInvalidID is returned when the input string is not a well-formed
	// SPIFFE URI: wrong scheme (anything other than spiffe), unparseable by
	// net/url.Parse, or contains forbidden URI components such as userinfo.
	ErrInvalidID = errors.New("spiffe: invalid id")

	// ErrInvalidTrustDomain is returned when the trust-domain (URI host)
	// component fails RFC 5234 host-shape validation: empty, longer than
	// 255 octets, contains uppercase letters, has a leading or trailing
	// dot/hyphen, contains a double-dot, or contains a character outside
	// [a-z0-9.-].
	ErrInvalidTrustDomain = errors.New("spiffe: invalid trust domain")

	// ErrInvalidPath is returned when the path component fails the SPIFFE
	// path constraints: empty, missing leading slash, longer than
	// maxPathLen, trailing slash, double slash, contains "..", contains a
	// raw or percent-encoded NUL byte, or the original URI carried a
	// query / fragment.
	ErrInvalidPath = errors.New("spiffe: invalid path")
)

// TrustDomain is a SPIFFE trust-domain identifier — the URI host portion
// of a SPIFFE ID. The validated form is always lowercase and matches the
// RFC 5234 host shape ([a-z0-9.-], no leading/trailing dot or hyphen,
// 1-255 octets, no consecutive dots).
//
// Construct via NewTrustDomain. Direct casting from string is permitted
// for caller convenience (e.g. test fixtures), but BuildSPIFFEID and
// related constructors re-validate defensively.
type TrustDomain string

// NewTrustDomain returns a validated TrustDomain or ErrInvalidTrustDomain.
func NewTrustDomain(s string) (TrustDomain, error) {
	if len(s) == 0 || len(s) > 255 {
		return "", ErrInvalidTrustDomain
	}
	// The SPIFFE spec is case-insensitive on trust domain but stores the
	// canonical form as lowercase — we require the caller to lowercase
	// upstream so the equality check (string compare) is meaningful.
	if s != strings.ToLower(s) {
		return "", ErrInvalidTrustDomain
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return "", ErrInvalidTrustDomain
	}
	if strings.Contains(s, "..") {
		return "", ErrInvalidTrustDomain
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-':
		default:
			return "", ErrInvalidTrustDomain
		}
	}
	// RFC 5234 host shape applies the no-leading/trailing-hyphen rule
	// per label (DNS LDH), not just per string — `aoid-.local` and
	// `-aoid.local` are both invalid because the label "aoid-" / "-aoid"
	// starts or ends with a hyphen.
	for _, label := range strings.Split(s, ".") {
		if label == "" {
			// Already caught by the leading/trailing-dot and double-dot
			// checks above, but defend defensively.
			return "", ErrInvalidTrustDomain
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", ErrInvalidTrustDomain
		}
	}
	return TrustDomain(s), nil
}

// SPIFFEID is a parsed and validated SPIFFE identifier.
//
// The zero value is NOT a valid SPIFFE ID — construct via ParseID or
// BuildSPIFFEID.
type SPIFFEID struct {
	// TrustDomain is the validated, lowercase trust-domain component.
	TrustDomain TrustDomain
	// Path is the validated path component, including its leading slash,
	// preserving original case (the SPIFFE spec mandates case-sensitive
	// path comparison).
	Path string
}

// maxPathLen caps the SPIFFE ID path component at 2048 bytes. The spec
// doesn't specify a numeric ceiling, but downstream consumers (Postgres
// columns, ASN.1 encoders, JWT claims) all benefit from a sane bound.
const maxPathLen = 2048

// ParseID parses a SPIFFE ID URI string and returns the validated
// SPIFFEID. Errors:
//
//   - ErrInvalidID — malformed URI, wrong scheme, or userinfo present.
//   - ErrInvalidTrustDomain — host component fails validation.
//   - ErrInvalidPath — path component fails validation OR the original
//     URI carried a query or fragment (the SPIFFE spec forbids both).
func ParseID(s string) (SPIFFEID, error) {
	u, err := url.Parse(s)
	if err != nil {
		return SPIFFEID{}, ErrInvalidID
	}
	if u.Scheme != "spiffe" {
		return SPIFFEID{}, ErrInvalidID
	}
	// Userinfo (user:pass@host) is forbidden by the SPIFFE-ID spec.
	if u.User != nil {
		return SPIFFEID{}, ErrInvalidID
	}
	// Opaque (`spiffe:foo`) is not the spec shape; require authority.
	if u.Opaque != "" {
		return SPIFFEID{}, ErrInvalidID
	}
	if u.Host == "" {
		return SPIFFEID{}, ErrInvalidTrustDomain
	}
	td, err := NewTrustDomain(u.Host)
	if err != nil {
		return SPIFFEID{}, err
	}
	if u.RawQuery != "" || u.ForceQuery {
		return SPIFFEID{}, ErrInvalidPath
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return SPIFFEID{}, ErrInvalidPath
	}
	// Prefer RawPath over Path when present: it preserves percent-encoded
	// octets (e.g. %00) that url.Parse would silently decode into the
	// Path field, defeating the percent-null check below. RawPath is
	// only populated when the path required encoding.
	p := u.Path
	if u.RawPath != "" {
		p = u.RawPath
	}
	if err := validatePath(p); err != nil {
		return SPIFFEID{}, err
	}
	// Store the decoded form so callers can use it as a stable map key.
	// We've already rejected encoded NUL above against the raw form.
	return SPIFFEID{TrustDomain: td, Path: u.Path}, nil
}

func validatePath(p string) error {
	if p == "" || !strings.HasPrefix(p, "/") {
		return ErrInvalidPath
	}
	if len(p) > maxPathLen {
		return ErrInvalidPath
	}
	if strings.HasSuffix(p, "/") {
		return ErrInvalidPath
	}
	if strings.Contains(p, "//") {
		return ErrInvalidPath
	}
	// Reject any ".." segment — guards path traversal whether the encoded
	// form is `/..` or `/foo/../bar`.
	if strings.Contains(p, "..") {
		return ErrInvalidPath
	}
	if strings.Contains(p, "\x00") {
		return ErrInvalidPath
	}
	// Percent-encoded NUL is a common encoded-traversal vector; reject
	// regardless of case (%00, %0A would still parse via decode but only
	// %00 is the NUL byte we care about here).
	if containsFold(p, "%00") {
		return ErrInvalidPath
	}
	return nil
}

// containsFold is a tiny ASCII case-insensitive Contains that avoids the
// strings.ToLower allocation for short needles.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// BuildSPIFFEID constructs a SPIFFEID from a TrustDomain and a path,
// applying the same validation rules ParseID applies — useful when the
// caller already holds a validated TrustDomain and wants to attach a
// programmatically constructed path (e.g. "/sa/" + workloadID).
//
// BuildSPIFFEID re-validates the supplied TrustDomain defensively in
// case the caller bypassed NewTrustDomain (TrustDomain is exported as a
// typed string for ergonomic literal use in tests / config).
func BuildSPIFFEID(td TrustDomain, path string) (SPIFFEID, error) {
	if _, err := NewTrustDomain(string(td)); err != nil {
		return SPIFFEID{}, err
	}
	if err := validatePath(path); err != nil {
		return SPIFFEID{}, err
	}
	return SPIFFEID{TrustDomain: td, Path: path}, nil
}

// URL returns a fresh *url.URL representing this SPIFFE ID, suitable for
// embedding in x509.Certificate.URIs at issuance time.
//
// Every call returns a distinct pointer so callers may mutate the
// returned value freely without aliasing the SPIFFEID's internal state.
func (id SPIFFEID) URL() *url.URL {
	return &url.URL{
		Scheme: "spiffe",
		Host:   string(id.TrustDomain),
		Path:   id.Path,
	}
}

// String returns the canonical SPIFFE URI form ("spiffe://<td><path>").
func (id SPIFFEID) String() string {
	return id.URL().String()
}
