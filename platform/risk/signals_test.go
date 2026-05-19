package risk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeHistorical is a deterministic HistoricalLookups for signal tests.
type fakeHistorical struct {
	mu              sync.Mutex
	attempts        []Attempt
	knownDevices    []string
	lastGeo         *GeoLocation
	isAnonIP        map[string]bool
	attemptsErr     error
	knownDevicesErr error
	lastGeoErr      error
	isAnonIPErr     error
}

func (f *fakeHistorical) RecentAttempts(_ context.Context, _ uuid.UUID, _ time.Duration) ([]Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attemptsErr != nil {
		return nil, f.attemptsErr
	}
	out := make([]Attempt, len(f.attempts))
	copy(out, f.attempts)
	return out, nil
}

func (f *fakeHistorical) KnownDeviceFingerprints(_ context.Context, _ uuid.UUID, _ time.Duration) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.knownDevicesErr != nil {
		return nil, f.knownDevicesErr
	}
	out := make([]string, len(f.knownDevices))
	copy(out, f.knownDevices)
	return out, nil
}

func (f *fakeHistorical) LastLoginGeo(_ context.Context, _ uuid.UUID) (*GeoLocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastGeoErr != nil {
		return nil, f.lastGeoErr
	}
	if f.lastGeo == nil {
		return nil, nil
	}
	cp := *f.lastGeo
	return &cp, nil
}

func (f *fakeHistorical) IsAnonymizerIP(_ context.Context, ip net.IP) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isAnonIPErr != nil {
		return false, f.isAnonIPErr
	}
	if f.isAnonIP == nil {
		return false, nil
	}
	return f.isAnonIP[ip.String()], nil
}

// fakeGeoIP serves a single canned response keyed by IP string.
type fakeGeoIP struct {
	mu      sync.Mutex
	by      map[string]*GeoLocation
	healthy bool
	err     error
}

func (g *fakeGeoIP) Lookup(ip net.IP) (*GeoLocation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err != nil {
		return nil, g.err
	}
	if ip == nil {
		return nil, errors.New("nil ip")
	}
	loc, ok := g.by[ip.String()]
	if !ok {
		return nil, nil
	}
	cp := *loc
	return &cp, nil
}

func (g *fakeGeoIP) RefreshedAt() time.Time { return time.Now() }
func (g *fakeGeoIP) Healthy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.healthy
}

// -------- GeoVelocityAnomalySignal --------

func TestSignal_GeoVelocityAnomaly_TriggersOnHighKMH(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{lastGeo: &GeoLocation{
		Lat: 40.7128, Lng: -74.0060, // NYC
		At: time.Now().Add(-1 * time.Hour),
	}}
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {Lat: 35.6762, Lng: 139.6503, At: time.Now()}, // Tokyo
	}}
	s := NewGeoVelocityAnomalySignal()
	triggered, weight, details := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if !triggered {
		t.Fatalf("expected triggered=true; details=%v", details)
	}
	if weight != 25 {
		t.Fatalf("want weight 25, got %d", weight)
	}
	if details["velocity_kmh"] == nil {
		t.Fatalf("expected velocity_kmh in details: %v", details)
	}
}

func TestSignal_GeoVelocityAnomaly_NoTrigger_NoHistory(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{} // lastGeo=nil
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {Lat: 35.6762, Lng: 139.6503, At: time.Now()},
	}}
	s := NewGeoVelocityAnomalySignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if triggered {
		t.Fatal("expected no trigger when no last_login_geo")
	}
}

func TestSignal_GeoVelocityAnomaly_NoTrigger_NilGeoIP(t *testing.T) {
	t.Parallel()
	s := NewGeoVelocityAnomalySignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		SourceIP:    "203.0.113.5",
		AttemptedAt: time.Now(),
	})
	if triggered {
		t.Fatal("expected no trigger when GeoIP nil")
	}
}

func TestSignal_GeoVelocityAnomaly_WithThresholdAndWeightOpts(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{lastGeo: &GeoLocation{
		Lat: 40.7128, Lng: -74.0060,
		At: time.Now().Add(-1 * time.Hour),
	}}
	// ~370 km away in 1 hour = ~370 km/h. Above the 100 km min-distance
	// floor and above the custom 50 km/h threshold below.
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {Lat: 44.0, Lng: -74.0, At: time.Now()},
	}}
	// Lower threshold to 50 km/h so it triggers.
	s := NewGeoVelocityAnomalySignal(WithVelocityThreshold(50), WithSignalWeight(30))
	triggered, weight, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if !triggered {
		t.Fatal("expected trigger with custom threshold")
	}
	if weight != 30 {
		t.Fatalf("want weight 30 (override), got %d", weight)
	}
}

// -------- NewGeoCountrySignal --------

func TestSignal_NewGeoCountry_TriggersWhenNeverSeen(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "success", CountryCode: "US", AttemptedAt: time.Now().Add(-24 * time.Hour)},
			{Outcome: "success", CountryCode: "US", AttemptedAt: time.Now().Add(-48 * time.Hour)},
		},
	}
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {CountryCode: "JP"},
	}}
	s := NewNewGeoCountrySignal()
	triggered, weight, details := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if !triggered {
		t.Fatalf("expected trigger; details=%v", details)
	}
	if weight != 15 {
		t.Fatalf("want weight 15, got %d", weight)
	}
	if details["country"] != "JP" {
		t.Fatalf("want country=JP, got %v", details["country"])
	}
}

func TestSignal_NewGeoCountry_NoTrigger_KnownCountry(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "success", CountryCode: "JP"},
		},
	}
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {CountryCode: "JP"},
	}}
	s := NewNewGeoCountrySignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if triggered {
		t.Fatal("expected no trigger for known country")
	}
}

func TestSignal_NewGeoCountry_NoTrigger_EmptyHistory(t *testing.T) {
	t.Parallel()
	// No prior attempts → can't determine "new" — must NOT trigger.
	hist := &fakeHistorical{}
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {CountryCode: "JP"},
	}}
	s := NewNewGeoCountrySignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if triggered {
		t.Fatal("expected no trigger when no historical attempts")
	}
}

// -------- NewDeviceFingerprintSignal --------

func TestSignal_NewDeviceFingerprint_TriggersOnUnknownPrint(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		knownDevices: []string{"deadbeef"},
	}
	s := NewNewDeviceFingerprintSignal()
	triggered, weight, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		UserAgent:         "Mozilla/5.0 (Macintosh; Intel Mac OS X)",
		AcceptLanguage:    "en-US,en;q=0.9",
		HistoricalLookups: hist,
	})
	if !triggered {
		t.Fatal("expected trigger for new fingerprint")
	}
	if weight != 10 {
		t.Fatalf("want weight 10, got %d", weight)
	}
}

func TestSignal_NewDeviceFingerprint_NoTrigger_Known(t *testing.T) {
	t.Parallel()
	// Pre-compute the fingerprint and seed knownDevices with it.
	ua := "Mozilla/5.0 (Macintosh)"
	al := "en-US"
	sum := sha256.Sum256([]byte(ua + "\x00" + al))
	fp := hex.EncodeToString(sum[:])
	hist := &fakeHistorical{knownDevices: []string{fp}}
	s := NewNewDeviceFingerprintSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		UserAgent:         ua,
		AcceptLanguage:    al,
		HistoricalLookups: hist,
	})
	if triggered {
		t.Fatal("expected no trigger for known fingerprint")
	}
}

func TestSignal_NewDeviceFingerprint_NoTrigger_EmptyUA(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{knownDevices: []string{"deadbeef"}}
	s := NewNewDeviceFingerprintSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		UserAgent:         "",
		HistoricalLookups: hist,
	})
	if triggered {
		t.Fatal("expected no trigger when UA empty (no false positives from missing data)")
	}
}

// -------- UAMismatchFromBaselineSignal --------

func TestSignal_UAMismatch_TriggersOnFamilyShift(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "success", UserAgent: "Mozilla/5.0 (Macintosh) Chrome/123"},
			{Outcome: "success", UserAgent: "Mozilla/5.0 (Macintosh) Chrome/124"},
		},
	}
	s := NewUAMismatchFromBaselineSignal()
	triggered, weight, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		UserAgent:         "curl/8.0",
		HistoricalLookups: hist,
	})
	if !triggered {
		t.Fatal("expected trigger on UA family shift")
	}
	if weight != 5 {
		t.Fatalf("want weight 5, got %d", weight)
	}
}

func TestSignal_UAMismatch_NoTrigger_SameFamily(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "success", UserAgent: "Mozilla/5.0 (Macintosh) Chrome/123"},
		},
	}
	s := NewUAMismatchFromBaselineSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		UserAgent:         "Mozilla/5.0 (Macintosh) Chrome/125",
		HistoricalLookups: hist,
	})
	if triggered {
		t.Fatal("expected no trigger in same Mozilla family")
	}
}

// -------- FailedAttemptRecencySignal --------

func TestSignal_FailedAttemptRecency_TriggersAtThreshold(t *testing.T) {
	t.Parallel()
	now := time.Now()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "failure", AttemptedAt: now.Add(-1 * time.Minute)},
			{Outcome: "failure", AttemptedAt: now.Add(-2 * time.Minute)},
			{Outcome: "failure", AttemptedAt: now.Add(-3 * time.Minute)},
			{Outcome: "success", AttemptedAt: now.Add(-4 * time.Minute)},  // ignored
			{Outcome: "failure", AttemptedAt: now.Add(-50 * time.Minute)}, // outside window
		},
	}
	s := NewFailedAttemptRecencySignal() // default: 3 failures in 15min
	triggered, weight, details := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		AttemptedAt:       now,
		HistoricalLookups: hist,
	})
	if !triggered {
		t.Fatalf("expected trigger at threshold; details=%v", details)
	}
	if weight != 15 {
		t.Fatalf("want weight 15, got %d", weight)
	}
	if details["failures"] != 3 {
		t.Fatalf("want 3 failures, got %v", details["failures"])
	}
}

func TestSignal_FailedAttemptRecency_NoTrigger_BelowThreshold(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "failure", AttemptedAt: time.Now().Add(-1 * time.Minute)},
		},
	}
	s := NewFailedAttemptRecencySignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
	})
	if triggered {
		t.Fatal("expected no trigger below threshold")
	}
}

func TestSignal_FailedAttemptRecency_CustomThreshold(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts: []Attempt{
			{Outcome: "failure", AttemptedAt: time.Now().Add(-1 * time.Minute)},
		},
	}
	s := NewFailedAttemptRecencySignal(WithFailureThreshold(1, 5*time.Minute))
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
	})
	if !triggered {
		t.Fatal("expected trigger at threshold=1")
	}
}

// -------- TorAnonymizerIPSignal --------

func TestSignal_TorAnonymizer_TriggersOnAnonIP(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{isAnonIP: map[string]bool{"198.51.100.7": true}}
	s := NewTorAnonymizerIPSignal()
	triggered, weight, _ := s.Evaluate(context.Background(), Request{
		SourceIP:          "198.51.100.7",
		HistoricalLookups: hist,
	})
	if !triggered {
		t.Fatal("expected trigger for known Tor exit node")
	}
	if weight != 30 {
		t.Fatalf("want weight 30, got %d", weight)
	}
}

func TestSignal_TorAnonymizer_NoTrigger_CleanIP(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{isAnonIP: map[string]bool{}}
	s := NewTorAnonymizerIPSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		SourceIP:          "203.0.113.5",
		HistoricalLookups: hist,
	})
	if triggered {
		t.Fatal("expected no trigger for clean IP")
	}
}

func TestSignal_TorAnonymizer_NoTrigger_InvalidIP(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{isAnonIP: map[string]bool{}}
	s := NewTorAnonymizerIPSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		SourceIP:          "not-an-ip",
		HistoricalLookups: hist,
	})
	if triggered {
		t.Fatal("expected no trigger for unparsable IP")
	}
}

// -------- ImpossibleTravelSignal --------

func TestSignal_ImpossibleTravel_TriggersAt900KMH(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{lastGeo: &GeoLocation{
		Lat: 40.7128, Lng: -74.0060, // NYC
		At: time.Now().Add(-1 * time.Hour),
	}}
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {Lat: 35.6762, Lng: 139.6503, At: time.Now()}, // Tokyo, 10,800 km/h
	}}
	s := NewImpossibleTravelSignal()
	triggered, weight, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if !triggered {
		t.Fatal("expected trigger at impossible travel velocity")
	}
	if weight != 40 {
		t.Fatalf("want weight 40, got %d", weight)
	}
}

func TestSignal_ImpossibleTravel_NoTrigger_PlausibleSpeed(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{lastGeo: &GeoLocation{
		Lat: 40.7128, Lng: -74.0060,
		At: time.Now().Add(-6 * time.Hour),
	}}
	// London is ~5,570 km from NYC; 6h gives ~928 km/h — JUST above the
	// commercial-aircraft baseline of 900 km/h. So we use San Francisco
	// (~4,130 km, ~688 km/h) which is well below threshold.
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {Lat: 37.7749, Lng: -122.4194, At: time.Now()},
	}}
	s := NewImpossibleTravelSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		AttemptedAt:       time.Now(),
		HistoricalLookups: hist,
		GeoIP:             geo,
	})
	if triggered {
		t.Fatal("expected no trigger for cross-country flight speed")
	}
}

// -------- MFABypassAttemptedSignal --------

func TestSignal_MFABypass_TriggersWhenRequiredButEmpty(t *testing.T) {
	t.Parallel()
	s := NewMFABypassAttemptedSignal()
	triggered, weight, _ := s.Evaluate(context.Background(), Request{
		PolicyContext: map[string]any{
			"mfa_required":          true,
			"mfa_factors_presented": 0,
		},
	})
	if !triggered {
		t.Fatal("expected trigger when MFA required and zero factors presented")
	}
	if weight != 50 {
		t.Fatalf("want weight 50, got %d", weight)
	}
}

func TestSignal_MFABypass_NoTrigger_MFANotRequired(t *testing.T) {
	t.Parallel()
	s := NewMFABypassAttemptedSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		PolicyContext: map[string]any{
			"mfa_required":          false,
			"mfa_factors_presented": 0,
		},
	})
	if triggered {
		t.Fatal("expected no trigger when MFA not required")
	}
}

func TestSignal_MFABypass_NoTrigger_FactorsPresented(t *testing.T) {
	t.Parallel()
	s := NewMFABypassAttemptedSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{
		PolicyContext: map[string]any{
			"mfa_required":          true,
			"mfa_factors_presented": 2,
		},
	})
	if triggered {
		t.Fatal("expected no trigger when factors were presented")
	}
}

func TestSignal_MFABypass_NoTrigger_NilPolicy(t *testing.T) {
	t.Parallel()
	s := NewMFABypassAttemptedSignal()
	triggered, _, _ := s.Evaluate(context.Background(), Request{})
	if triggered {
		t.Fatal("expected no trigger when policy context nil")
	}
}

// -------- Cross-cutting --------

func TestAllSignals_NoPanicOnZeroRequest(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		NewGeoVelocityAnomalySignal(),
		NewNewGeoCountrySignal(),
		NewNewDeviceFingerprintSignal(),
		NewUAMismatchFromBaselineSignal(),
		NewFailedAttemptRecencySignal(),
		NewTorAnonymizerIPSignal(),
		NewImpossibleTravelSignal(),
		NewMFABypassAttemptedSignal(),
	}
	for _, s := range signals {
		s := s
		t.Run(s.Name(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked on zero request: %v", s.Name(), r)
				}
			}()
			triggered, _, _ := s.Evaluate(context.Background(), Request{})
			if triggered {
				t.Fatalf("%s triggered on zero request — false positive from missing data", s.Name())
			}
		})
	}
}

func TestAllSignals_ConcurrentEvaluateSafe(t *testing.T) {
	t.Parallel()
	hist := &fakeHistorical{
		attempts:     []Attempt{{Outcome: "success", CountryCode: "US"}},
		knownDevices: []string{"deadbeef"},
		lastGeo:      &GeoLocation{Lat: 40.7128, Lng: -74.0060, At: time.Now().Add(-1 * time.Hour)},
		isAnonIP:     map[string]bool{},
	}
	geo := &fakeGeoIP{healthy: true, by: map[string]*GeoLocation{
		"203.0.113.5": {CountryCode: "JP", Lat: 35.6762, Lng: 139.6503},
	}}
	signals := []Signal{
		NewGeoVelocityAnomalySignal(),
		NewNewGeoCountrySignal(),
		NewNewDeviceFingerprintSignal(),
		NewUAMismatchFromBaselineSignal(),
		NewFailedAttemptRecencySignal(),
		NewTorAnonymizerIPSignal(),
		NewImpossibleTravelSignal(),
		NewMFABypassAttemptedSignal(),
	}
	e := NewEvaluator(signals)
	req := Request{
		AccountID:         uuid.New(),
		SourceIP:          "203.0.113.5",
		UserAgent:         "Mozilla/5.0",
		AcceptLanguage:    "en-US",
		AttemptedAt:       time.Now(),
		PolicyContext:     map[string]any{"mfa_required": true, "mfa_factors_presented": 2},
		HistoricalLookups: hist,
		GeoIP:             geo,
	}
	var wg sync.WaitGroup
	workers := runtime.GOMAXPROCS(0) * 4
	if workers < 8 {
		workers = 8
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = e.Eval(context.Background(), req)
			}
		}()
	}
	wg.Wait()
}

func TestHaversineKM(t *testing.T) {
	t.Parallel()
	// NYC -> Tokyo ~ 10,800 km
	d := haversineKM(40.7128, -74.0060, 35.6762, 139.6503)
	if d < 10000 || d > 11500 {
		t.Fatalf("NYC-Tokyo: want ~10800 km, got %.1f", d)
	}
	// Same point = 0
	if z := haversineKM(40.7128, -74.0060, 40.7128, -74.0060); z != 0 {
		t.Fatalf("same point: want 0, got %f", z)
	}
}
