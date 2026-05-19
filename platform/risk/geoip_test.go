package risk

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	mmdbwriter "github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func TestMaxMindGeoIP_MissingDB_FallsBackToNoOp(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist.mmdb")
	lookup := NewMaxMindGeoIP(missing)
	if lookup == nil {
		t.Fatal("NewMaxMindGeoIP must never return nil (returns NoOpGeoIP on missing DB)")
	}
	if _, ok := lookup.(*NoOpGeoIP); !ok {
		t.Fatalf("expected NoOpGeoIP when DB missing, got %T", lookup)
	}
	if lookup.Healthy() {
		t.Fatal("NoOpGeoIP must report Healthy()=false")
	}
}

func TestMaxMindGeoIP_CorruptDB_FallsBackToNoOp(t *testing.T) {
	t.Parallel()
	corrupt := filepath.Join(t.TempDir(), "corrupt.mmdb")
	if err := os.WriteFile(corrupt, []byte("not an mmdb file"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	lookup := NewMaxMindGeoIP(corrupt)
	if _, ok := lookup.(*NoOpGeoIP); !ok {
		t.Fatalf("expected NoOpGeoIP fallback on corrupt DB, got %T", lookup)
	}
	if lookup.Healthy() {
		t.Fatal("NoOpGeoIP must report Healthy()=false")
	}
}

func TestNoOpGeoIP_AlwaysNilLookup(t *testing.T) {
	t.Parallel()
	n := &NoOpGeoIP{}
	loc, err := n.Lookup(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Fatalf("NoOpGeoIP.Lookup must not error: %v", err)
	}
	if loc != nil {
		t.Fatal("NoOpGeoIP.Lookup must return nil location")
	}
	if n.Healthy() {
		t.Fatal("NoOpGeoIP.Healthy must be false")
	}
	if !n.RefreshedAt().IsZero() {
		t.Fatal("NoOpGeoIP.RefreshedAt must be zero-time")
	}
}

func TestMaxMindGeoIP_LookupValidIP(t *testing.T) {
	t.Parallel()
	dbPath := buildTestMMDB(t, "1.2.3.0/24", "US", "New York", 40.7128, -74.0060, 10)
	lookup := NewMaxMindGeoIP(dbPath)
	if _, ok := lookup.(*MaxMindGeoIP); !ok {
		t.Fatalf("expected MaxMindGeoIP from valid DB, got %T", lookup)
	}
	if !lookup.Healthy() {
		t.Fatal("Healthy() must be true after valid open")
	}
	if lookup.RefreshedAt().IsZero() {
		t.Fatal("RefreshedAt() must be populated")
	}

	loc, err := lookup.Lookup(net.ParseIP("1.2.3.4"))
	if err != nil {
		t.Fatalf("Lookup err: %v", err)
	}
	if loc == nil {
		t.Fatal("expected non-nil location for in-range IP")
	}
	if loc.CountryCode != "US" {
		t.Fatalf("want CountryCode=US, got %q", loc.CountryCode)
	}
	if loc.City != "New York" {
		t.Fatalf("want City=New York, got %q", loc.City)
	}
	if loc.Lat < 40 || loc.Lat > 41 {
		t.Fatalf("want Lat~40.7, got %f", loc.Lat)
	}
	if loc.Lng > -73 || loc.Lng < -75 {
		t.Fatalf("want Lng~-74.0, got %f", loc.Lng)
	}
	if loc.AccuracyKM != 10 {
		t.Fatalf("want AccuracyKM=10, got %d", loc.AccuracyKM)
	}
	if loc.At.IsZero() {
		t.Fatal("MaxMindGeoIP.Lookup must stamp Location.At")
	}
}

func TestMaxMindGeoIP_LookupNilIP(t *testing.T) {
	t.Parallel()
	dbPath := buildTestMMDB(t, "1.2.3.0/24", "US", "NYC", 40.7, -74.0, 10)
	lookup := NewMaxMindGeoIP(dbPath)
	if _, err := lookup.Lookup(nil); err == nil {
		t.Fatal("nil IP must return an error")
	}
}

func TestMaxMindGeoIP_LookupNotFound(t *testing.T) {
	t.Parallel()
	dbPath := buildTestMMDB(t, "1.2.3.0/24", "US", "NYC", 40.7, -74.0, 10)
	lookup := NewMaxMindGeoIP(dbPath)
	loc, err := lookup.Lookup(net.ParseIP("9.9.9.9"))
	if err != nil {
		t.Fatalf("not-found IP should not error: %v", err)
	}
	if loc != nil {
		t.Fatal("IP outside seeded network should return nil location")
	}
}

func TestMaxMindGeoIP_Close(t *testing.T) {
	t.Parallel()
	dbPath := buildTestMMDB(t, "1.2.3.0/24", "US", "NYC", 40.7, -74.0, 10)
	lookup := NewMaxMindGeoIP(dbPath)
	mm, ok := lookup.(*MaxMindGeoIP)
	if !ok {
		t.Skip("not a MaxMindGeoIP")
	}
	if err := mm.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if mm.Healthy() {
		t.Fatal("closed reader must report Healthy()=false")
	}
	// Second close should be a no-op.
	if err := mm.Close(); err != nil {
		t.Fatalf("double close should be safe: %v", err)
	}
}

// buildTestMMDB writes a minimal GeoIP2-City-shaped mmdb file with a single
// network and returns its path. Tests build their fixtures lazily so we don't
// vendor any MaxMind binary blob.
func buildTestMMDB(t *testing.T, cidr, country, city string, lat, lng float64, accuracy int32) string {
	t.Helper()
	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoIP2-City",
		RecordSize:   24,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", cidr, err)
	}
	record := mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code": mmdbtype.String(country),
		},
		"city": mmdbtype.Map{
			"names": mmdbtype.Map{
				"en": mmdbtype.String(city),
			},
		},
		"location": mmdbtype.Map{
			"latitude":        mmdbtype.Float64(lat),
			"longitude":       mmdbtype.Float64(lng),
			"accuracy_radius": mmdbtype.Uint16(uint16(accuracy)),
		},
	}
	if err := w.Insert(network, record); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "test.mmdb")
	if err := os.WriteFile(dbPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Make sure RefreshedAt picks up a non-zero ModTime.
	_ = os.Chtimes(dbPath, time.Now(), time.Now())
	return dbPath
}
