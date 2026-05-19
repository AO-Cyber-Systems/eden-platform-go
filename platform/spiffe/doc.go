// Package spiffe provides the minimum SPIFFE-spec surface needed for
// AOID and other Eden consumers to issue SPIRE-compatible X.509-SVIDs
// without depending on the heavy github.com/spiffe/go-spiffe SDK.
//
// Scope:
//
//   - TrustDomain — typed string with NewTrustDomain validator enforcing
//     RFC 5234 host shape (lowercase letters, digits, dots, hyphens;
//     1-255 octets; no leading/trailing dot or hyphen; no consecutive
//     dots).
//   - SPIFFEID — typed struct {TrustDomain, Path}; ParseID(s) parses
//     spiffe://<td>/<path> URIs and rejects every non-conforming shape
//     (non-spiffe scheme, userinfo, empty trust domain, empty path,
//     missing leading slash, query, fragment, double slash, trailing
//     slash, path traversal, raw or percent-encoded NUL bytes, > 2048
//     octets).
//   - SPIFFEID.URL — returns a *url.URL with Scheme=spiffe, Host=td,
//     Path=path, suitable for embedding in x509.Certificate.URIs.
//   - BuildSPIFFEID — convenience constructor from validated TrustDomain
//     plus path; re-validates both defensively.
//   - LeafTemplateSVID — returns a fresh *x509.Certificate template per
//     SPIFFE X.509-SVID spec §3 (empty Subject, exactly one URI SAN, no
//     DNS / Email / IP SANs, KeyUsage=DigitalSignature, ExtKeyUsage =
//     [ClientAuth, ServerAuth], IsCA=false, BasicConstraintsValid=true,
//     SerialNumber=nil — caller assigns at signing time).
//   - MaxSVIDTTL = 24h; DefaultSVIDTTL = 1h.
//
// The package does NOT sign certificates. Pair it with platform/pki.CA
// for issuance — pki.CA.IssueLeafFromCSR consumes the template returned
// here.
//
// Example (caller in AOID's SvidService):
//
//	id, err := spiffe.ParseID(fmt.Sprintf("spiffe://%s/sa/%s", td, workload))
//	if err != nil {
//	    return err
//	}
//	notBefore := time.Now().Add(-5 * time.Minute) // backdate for clock skew
//	notAfter := notBefore.Add(spiffe.DefaultSVIDTTL)
//	tmpl := spiffe.LeafTemplateSVID(id, notBefore, notAfter)
//	der, cert, err := ca.IssueLeafFromCSR(ctx, csrDER, tmpl)
//
// Design rationale (why not github.com/spiffe/go-spiffe?):
//
// The SPIRE go-spiffe SDK drags in the SPIRE workload-API client, gRPC
// dependencies, and a tlsconfig package none of which AOID uses. AOID
// does NOT run a SPIRE server; it issues SVIDs natively from its own CA
// (platform/pki.CA), and clients consume those SVIDs via standard mTLS.
// The total surface needed for issuance is < 300 LOC, so we ship it
// in-house rather than take the dependency.
//
// References:
//
//   - SPIFFE-ID spec:    https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE-ID.md
//   - X.509-SVID spec:   https://github.com/spiffe/spiffe/blob/main/standards/X509-SVID.md
package spiffe
