package risk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// Per-signal functional options
// =============================================================================

// SignalOpt is a generic per-signal configuration setter. Each signal exposes
// its own option helpers (WithVelocityThreshold, WithFailureThreshold, etc.).
// WithSignalWeight overrides a signal's weight for per-tenant tuning.
type SignalOpt interface {
	apply(any)
}

type optFn func(any)

func (f optFn) apply(s any) { f(s) }

// WithSignalWeight overrides a signal's default weight. Applies to every
// signal in this package.
func WithSignalWeight(w int32) SignalOpt {
	return optFn(func(s any) {
		type weighted interface{ setWeight(int32) }
		if wx, ok := s.(weighted); ok {
			wx.setWeight(w)
		}
	})
}

// WithVelocityThreshold sets the km/h threshold for the geo-velocity signal
// (default 900 km/h).
func WithVelocityThreshold(kmh float64) SignalOpt {
	return optFn(func(s any) {
		if gv, ok := s.(*geoVelocityAnomalySignal); ok {
			gv.thresholdKMH = kmh
		}
		if it, ok := s.(*impossibleTravelSignal); ok {
			it.thresholdKMH = kmh
		}
	})
}

// WithFailureThreshold sets the (failures, window) threshold for the
// failed-attempt-recency signal.
func WithFailureThreshold(failures int, window time.Duration) SignalOpt {
	return optFn(func(s any) {
		if f, ok := s.(*failedAttemptRecencySignal); ok {
			f.threshold = failures
			f.window = window
		}
	})
}

// WithHistoryWindow sets how far back signals scan for history-based
// evaluation (defaults vary per signal).
func WithHistoryWindow(d time.Duration) SignalOpt {
	return optFn(func(s any) {
		type windowed interface{ setWindow(time.Duration) }
		if w, ok := s.(windowed); ok {
			w.setWindow(d)
		}
	})
}

// applyOpts is shared by every constructor.
func applyOpts(target any, opts []SignalOpt) {
	for _, o := range opts {
		if o != nil {
			o.apply(target)
		}
	}
}

// =============================================================================
// 1. geo_velocity_anomaly  (weight 25, default 900 km/h)
// =============================================================================

type geoVelocityAnomalySignal struct {
	weight       int32
	thresholdKMH float64
	minDistKM    float64 // ignore noise below this (default 100 km)
}

func (s *geoVelocityAnomalySignal) setWeight(w int32) { s.weight = w }

// NewGeoVelocityAnomalySignal returns the standard geo-velocity signal.
// Default weight 25, default threshold 900 km/h.
func NewGeoVelocityAnomalySignal(opts ...SignalOpt) Signal {
	s := &geoVelocityAnomalySignal{weight: 25, thresholdKMH: 900, minDistKM: 100}
	applyOpts(s, opts)
	return s
}

func (s *geoVelocityAnomalySignal) Name() string { return "geo_velocity_anomaly" }

func (s *geoVelocityAnomalySignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	if req.GeoIP == nil || req.HistoricalLookups == nil {
		return false, 0, nil
	}
	currentIP := net.ParseIP(req.SourceIP)
	if currentIP == nil {
		return false, 0, nil
	}
	currentGeo, err := req.GeoIP.Lookup(currentIP)
	if err != nil || currentGeo == nil {
		return false, 0, nil
	}
	if req.AccountID == uuid.Nil {
		return false, 0, nil
	}
	lastGeo, err := req.HistoricalLookups.LastLoginGeo(ctx, req.AccountID)
	if err != nil || lastGeo == nil {
		return false, 0, nil
	}
	distKM := haversineKM(lastGeo.Lat, lastGeo.Lng, currentGeo.Lat, currentGeo.Lng)
	hours := req.AttemptedAt.Sub(lastGeo.At).Hours()
	if hours <= 0 || distKM < s.minDistKM {
		return false, 0, nil
	}
	kmh := distKM / hours
	if kmh < s.thresholdKMH {
		return false, 0, nil
	}
	return true, s.weight, map[string]any{
		"distance_km":  distKM,
		"delta_hours":  hours,
		"velocity_kmh": kmh,
		"from_country": lastGeo.CountryCode,
		"to_country":   currentGeo.CountryCode,
	}
}

// =============================================================================
// 2. new_geo_country  (weight 15)
// =============================================================================

type newGeoCountrySignal struct {
	weight int32
	window time.Duration
}

func (s *newGeoCountrySignal) setWeight(w int32)         { s.weight = w }
func (s *newGeoCountrySignal) setWindow(d time.Duration) { s.window = d }

// NewNewGeoCountrySignal triggers when the current attempt's country has no
// prior recorded attempt for this account inside the history window.
//
// IMPORTANT: triggers only if HistoricalLookups returns a non-empty attempt
// list. An account with zero history cannot have a "new" country (this
// prevents false positives on first-ever login).
func NewNewGeoCountrySignal(opts ...SignalOpt) Signal {
	s := &newGeoCountrySignal{weight: 15, window: 90 * 24 * time.Hour}
	applyOpts(s, opts)
	return s
}

func (s *newGeoCountrySignal) Name() string { return "new_geo_country" }

func (s *newGeoCountrySignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	if req.GeoIP == nil || req.HistoricalLookups == nil {
		return false, 0, nil
	}
	currentIP := net.ParseIP(req.SourceIP)
	if currentIP == nil {
		return false, 0, nil
	}
	currentGeo, err := req.GeoIP.Lookup(currentIP)
	if err != nil || currentGeo == nil || currentGeo.CountryCode == "" {
		return false, 0, nil
	}
	if req.AccountID == uuid.Nil {
		return false, 0, nil
	}
	attempts, err := req.HistoricalLookups.RecentAttempts(ctx, req.AccountID, s.window)
	if err != nil || len(attempts) == 0 {
		// No history → cannot deem "new". Avoid false positives.
		return false, 0, nil
	}
	for _, a := range attempts {
		if a.CountryCode == currentGeo.CountryCode {
			return false, 0, nil
		}
	}
	return true, s.weight, map[string]any{
		"country":     currentGeo.CountryCode,
		"history_len": len(attempts),
	}
}

// =============================================================================
// 3. new_device_fingerprint  (weight 10)
// =============================================================================

type newDeviceFingerprintSignal struct {
	weight int32
	window time.Duration
}

func (s *newDeviceFingerprintSignal) setWeight(w int32)         { s.weight = w }
func (s *newDeviceFingerprintSignal) setWindow(d time.Duration) { s.window = d }

// NewNewDeviceFingerprintSignal triggers when the current attempt's
// fingerprint (sha256 of UA + Accept-Language) is not in the account's known
// device set within the configured window.
func NewNewDeviceFingerprintSignal(opts ...SignalOpt) Signal {
	s := &newDeviceFingerprintSignal{weight: 10, window: 90 * 24 * time.Hour}
	applyOpts(s, opts)
	return s
}

func (s *newDeviceFingerprintSignal) Name() string { return "new_device_fingerprint" }

func (s *newDeviceFingerprintSignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	if req.HistoricalLookups == nil {
		return false, 0, nil
	}
	if req.UserAgent == "" {
		// Missing data ≠ new device. Avoid false positives.
		return false, 0, nil
	}
	if req.AccountID == uuid.Nil {
		return false, 0, nil
	}
	fp := deviceFingerprint(req.UserAgent, req.AcceptLanguage)
	known, err := req.HistoricalLookups.KnownDeviceFingerprints(ctx, req.AccountID, s.window)
	if err != nil {
		return false, 0, nil
	}
	for _, k := range known {
		if k == fp {
			return false, 0, nil
		}
	}
	return true, s.weight, map[string]any{
		"fingerprint": fp,
		"known_count": len(known),
	}
}

// deviceFingerprint is sha256("<UA>\x00<AcceptLanguage>") hex-encoded.
func deviceFingerprint(ua, al string) string {
	sum := sha256.Sum256([]byte(ua + "\x00" + al))
	return hex.EncodeToString(sum[:])
}

// =============================================================================
// 4. ua_mismatch_from_baseline  (weight 5)
// =============================================================================

type uaMismatchFromBaselineSignal struct {
	weight int32
	window time.Duration
}

func (s *uaMismatchFromBaselineSignal) setWeight(w int32)         { s.weight = w }
func (s *uaMismatchFromBaselineSignal) setWindow(d time.Duration) { s.window = d }

// NewUAMismatchFromBaselineSignal triggers when the current User-Agent's
// "family token" (first token of the UA string) doesn't match any recent
// attempt's family token.
func NewUAMismatchFromBaselineSignal(opts ...SignalOpt) Signal {
	s := &uaMismatchFromBaselineSignal{weight: 5, window: 30 * 24 * time.Hour}
	applyOpts(s, opts)
	return s
}

func (s *uaMismatchFromBaselineSignal) Name() string { return "ua_mismatch_from_baseline" }

func (s *uaMismatchFromBaselineSignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	if req.HistoricalLookups == nil || req.UserAgent == "" {
		return false, 0, nil
	}
	if req.AccountID == uuid.Nil {
		return false, 0, nil
	}
	cur := uaFamilyToken(req.UserAgent)
	if cur == "" {
		return false, 0, nil
	}
	attempts, err := req.HistoricalLookups.RecentAttempts(ctx, req.AccountID, s.window)
	if err != nil || len(attempts) == 0 {
		return false, 0, nil
	}
	for _, a := range attempts {
		if uaFamilyToken(a.UserAgent) == cur {
			return false, 0, nil
		}
	}
	return true, s.weight, map[string]any{
		"current_family": cur,
	}
}

// uaFamilyToken returns the first whitespace-delimited token of the UA, with
// the version suffix stripped. Crude but adequate for "Mozilla/5.0" vs
// "curl/8.0" family discrimination.
func uaFamilyToken(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return ""
	}
	tok := ua
	if i := strings.IndexAny(ua, " \t"); i > 0 {
		tok = ua[:i]
	}
	if i := strings.Index(tok, "/"); i > 0 {
		tok = tok[:i]
	}
	return strings.ToLower(tok)
}

// =============================================================================
// 5. failed_attempt_recency  (weight 15, default 3-in-15min)
// =============================================================================

type failedAttemptRecencySignal struct {
	weight    int32
	threshold int
	window    time.Duration
}

func (s *failedAttemptRecencySignal) setWeight(w int32)         { s.weight = w }
func (s *failedAttemptRecencySignal) setWindow(d time.Duration) { s.window = d }

// NewFailedAttemptRecencySignal triggers when the account has had at least
// `threshold` failed auth attempts within `window`.
func NewFailedAttemptRecencySignal(opts ...SignalOpt) Signal {
	s := &failedAttemptRecencySignal{weight: 15, threshold: 3, window: 15 * time.Minute}
	applyOpts(s, opts)
	return s
}

func (s *failedAttemptRecencySignal) Name() string { return "failed_attempt_recency" }

func (s *failedAttemptRecencySignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	if req.HistoricalLookups == nil {
		return false, 0, nil
	}
	if req.AccountID == uuid.Nil {
		return false, 0, nil
	}
	cutoff := req.AttemptedAt.Add(-s.window)
	if req.AttemptedAt.IsZero() {
		cutoff = time.Now().Add(-s.window)
	}
	attempts, err := req.HistoricalLookups.RecentAttempts(ctx, req.AccountID, s.window)
	if err != nil {
		return false, 0, nil
	}
	count := 0
	for _, a := range attempts {
		if a.Outcome != "failure" {
			continue
		}
		if a.AttemptedAt.Before(cutoff) {
			continue
		}
		count++
	}
	if count < s.threshold {
		return false, 0, nil
	}
	return true, s.weight, map[string]any{
		"failures":   count,
		"window_min": s.window.Minutes(),
	}
}

// =============================================================================
// 6. tor_anonymizer_ip  (weight 30)
// =============================================================================

type torAnonymizerIPSignal struct {
	weight int32
}

func (s *torAnonymizerIPSignal) setWeight(w int32) { s.weight = w }

// NewTorAnonymizerIPSignal triggers when SourceIP is on the operator's
// anonymizer list (consulted via HistoricalLookups.IsAnonymizerIP).
func NewTorAnonymizerIPSignal(opts ...SignalOpt) Signal {
	s := &torAnonymizerIPSignal{weight: 30}
	applyOpts(s, opts)
	return s
}

func (s *torAnonymizerIPSignal) Name() string { return "tor_anonymizer_ip" }

func (s *torAnonymizerIPSignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	if req.HistoricalLookups == nil {
		return false, 0, nil
	}
	ip := net.ParseIP(req.SourceIP)
	if ip == nil {
		return false, 0, nil
	}
	is, err := req.HistoricalLookups.IsAnonymizerIP(ctx, ip)
	if err != nil || !is {
		return false, 0, nil
	}
	return true, s.weight, map[string]any{
		"source_ip": ip.String(),
	}
}

// =============================================================================
// 7. impossible_travel  (weight 40)
// =============================================================================

type impossibleTravelSignal struct {
	weight       int32
	thresholdKMH float64
	minDistKM    float64
}

func (s *impossibleTravelSignal) setWeight(w int32) { s.weight = w }

// NewImpossibleTravelSignal is a specialization of geo-velocity at the
// >900 km/h commercial-aircraft baseline. It triggers IN ADDITION to
// geo-velocity (when both are wired) so callers see escalation in the
// triggered list.
func NewImpossibleTravelSignal(opts ...SignalOpt) Signal {
	s := &impossibleTravelSignal{weight: 40, thresholdKMH: 900, minDistKM: 100}
	applyOpts(s, opts)
	return s
}

func (s *impossibleTravelSignal) Name() string { return "impossible_travel" }

func (s *impossibleTravelSignal) Evaluate(ctx context.Context, req Request) (bool, int32, map[string]any) {
	// Same logic as geo-velocity — separate signal so it appears in the
	// triggered list at its higher weight.
	if req.GeoIP == nil || req.HistoricalLookups == nil {
		return false, 0, nil
	}
	currentIP := net.ParseIP(req.SourceIP)
	if currentIP == nil {
		return false, 0, nil
	}
	currentGeo, err := req.GeoIP.Lookup(currentIP)
	if err != nil || currentGeo == nil {
		return false, 0, nil
	}
	if req.AccountID == uuid.Nil {
		return false, 0, nil
	}
	lastGeo, err := req.HistoricalLookups.LastLoginGeo(ctx, req.AccountID)
	if err != nil || lastGeo == nil {
		return false, 0, nil
	}
	distKM := haversineKM(lastGeo.Lat, lastGeo.Lng, currentGeo.Lat, currentGeo.Lng)
	hours := req.AttemptedAt.Sub(lastGeo.At).Hours()
	if hours <= 0 || distKM < s.minDistKM {
		return false, 0, nil
	}
	kmh := distKM / hours
	if kmh < s.thresholdKMH {
		return false, 0, nil
	}
	return true, s.weight, map[string]any{
		"distance_km":  distKM,
		"delta_hours":  hours,
		"velocity_kmh": kmh,
		"from_country": lastGeo.CountryCode,
		"to_country":   currentGeo.CountryCode,
	}
}

// =============================================================================
// 8. mfa_bypass_attempted  (weight 50)
// =============================================================================

type mfaBypassAttemptedSignal struct {
	weight int32
}

func (s *mfaBypassAttemptedSignal) setWeight(w int32) { s.weight = w }

// NewMFABypassAttemptedSignal triggers when tenant policy requires MFA but
// the request presented zero factors. Caller must populate
// PolicyContext["mfa_required"]=true and PolicyContext["mfa_factors_presented"]
// (int, may be 0).
func NewMFABypassAttemptedSignal(opts ...SignalOpt) Signal {
	s := &mfaBypassAttemptedSignal{weight: 50}
	applyOpts(s, opts)
	return s
}

func (s *mfaBypassAttemptedSignal) Name() string { return "mfa_bypass_attempted" }

func (s *mfaBypassAttemptedSignal) Evaluate(_ context.Context, req Request) (bool, int32, map[string]any) {
	if req.PolicyContext == nil {
		return false, 0, nil
	}
	req1, ok := req.PolicyContext["mfa_required"].(bool)
	if !ok || !req1 {
		return false, 0, nil
	}
	presented, _ := intValue(req.PolicyContext["mfa_factors_presented"])
	if presented > 0 {
		return false, 0, nil
	}
	return true, s.weight, map[string]any{
		"mfa_required":          true,
		"mfa_factors_presented": 0,
	}
}

// intValue tolerantly extracts an int from any numeric type stored in
// PolicyContext. JSON-decoded maps typically yield float64.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// =============================================================================
// haversineKM — great-circle distance in kilometers.
// =============================================================================

// haversineKM computes the great-circle distance between two points using
// the haversine formula. Result in kilometers.
func haversineKM(lat1, lng1, lat2, lng2 float64) float64 {
	if lat1 == lat2 && lng1 == lng2 {
		return 0
	}
	const earthRadiusKM = 6371.0
	toRad := func(d float64) float64 { return d * math.Pi / 180.0 }
	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	dphi := toRad(lat2 - lat1)
	dlam := toRad(lng2 - lng1)
	a := math.Sin(dphi/2)*math.Sin(dphi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dlam/2)*math.Sin(dlam/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKM * c
}
