// Package breach provides NIST SP 800-63B Rev 4 §5.1.1 breach-corpus
// password screening as swappable Screener implementations.
//
// Consumers (AOID, AODex, eden-biz, AOSentry, AOFamily, …) select the
// implementation per-tenant or per-deployment to satisfy the §5.1.1
// requirement that "verifiers SHALL compare prospective secrets against
// a list that contains values known to be commonly used, expected, or
// compromised."
//
// # Choosing an implementation
//
//   - HIBPScreener — cloud-hosted, k-anonymity protocol against
//     api.pwnedpasswords.com. Password never leaves the host; only the
//     first 5 hex chars of SHA-1(password) cross the wire. Preferred for
//     SOC 2 / CMMC L2 / FedRAMP Moderate deployments with allowed egress.
//
//   - LocalListScreener — embedded top-10k common-password corpus
//     (xato-net, lowercased + sort-uniqued at build time). No network
//     egress; binary-search lookup. Preferred for FedRAMP High and
//     air-gapped deployments. Note this is a baseline, not exhaustive —
//     combine with HIBPScreener where policy allows.
//
//   - DisabledScreener — no-op. Use only when policy explicitly waives
//     breach screening and the remaining controls (Argon2id/PBKDF2-SHA256,
//     length floor, MFA) are deemed sufficient by the AO/3PAO.
//
// # SHA-1 disclaimer
//
// HIBPScreener uses SHA-1 internally as the k-anonymity prefix protocol
// mandated by HIBP's PwnedPasswords API. This is NOT a security boundary;
// password authentication relies on Argon2id (or PBKDF2-SHA256 in FIPS
// mode) for hashing. The SHA-1 use here is equivalent to a hash-table
// shard key — collision resistance is irrelevant to the protocol.
// Do NOT replace SHA-1 in this package with SHA-256; doing so would break
// the HIBP API contract and silently disable breach screening.
//
// # Fail-open semantics
//
// HIBPScreener fails open on transient errors — Check returns
// (false, 0, ErrScreenerUnavailable) without rejecting the password.
// This is defense-in-depth: NIST 800-63B breach screening is one
// control of several, and a third-party API outage MUST NOT prevent
// legitimate logins (denial-of-service).
//
// Callers MUST audit ErrScreenerUnavailable occurrences for trend
// analysis: a sustained outage indicates either a HIBP problem or a
// network/proxy issue worth investigating.
//
// LocalListScreener and DisabledScreener cannot fail-open because they
// have no external dependency — they always return a definitive
// (compromised, count, nil) tuple.
//
// # Thread safety
//
// All three implementations are safe for concurrent use. HIBPScreener
// shares its underlying http.Client and rate.Limiter across goroutines;
// LocalListScreener's password slice is loaded once and never mutated.
package breach
