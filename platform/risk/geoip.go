package risk

import (
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// GeoIPLookup is the contract signals use to resolve a source IP to a
// GeoLocation. Two implementations ship in this package:
//
//   - *MaxMindGeoIP — production: backs onto a memory-mapped MaxMind DB file.
//   - *NoOpGeoIP    — fallback: returns (nil, nil) for every lookup. Used in
//     air-gapped IL5 deployments where no DB file is mounted.
//
// NewMaxMindGeoIP guarantees a non-nil GeoIPLookup return — it falls back to
// NoOpGeoIP on file-missing or corrupt-DB conditions. Geo-aware signals MUST
// nil-check Lookup's (*GeoLocation) return.
type GeoIPLookup interface {
	Lookup(ip net.IP) (*GeoLocation, error)
	RefreshedAt() time.Time
	Healthy() bool
}

// MaxMindGeoIP is the production implementation of GeoIPLookup. It wraps a
// memory-mapped GeoLite2-City.mmdb (or compatible) file via
// oschwald/maxminddb-golang/v2 and is safe for concurrent use.
//
// Operators supply the database file out of band — see the package README
// for details on procurement and licensing.
type MaxMindGeoIP struct {
	mu          sync.RWMutex
	reader      *maxminddb.Reader
	dbPath      string
	refreshedAt time.Time
}

// NewMaxMindGeoIP opens the database at dbPath. If the file is missing,
// corrupt, or otherwise unopenable, it returns a *NoOpGeoIP instead — the
// caller never gets a nil interface. The degradation is logged via slog.Warn.
//
// This contract matters in air-gapped IL5 deployments where the MaxMind DB
// might not be present; geo-aware signals will then no-op without affecting
// availability.
func NewMaxMindGeoIP(dbPath string) GeoIPLookup {
	reader, err := maxminddb.Open(dbPath)
	if err != nil {
		slog.Warn("risk geoip: db unavailable; falling back to no-op",
			"path", dbPath, "error", err)
		return &NoOpGeoIP{}
	}
	refreshed := time.Now()
	if stat, statErr := os.Stat(dbPath); statErr == nil {
		refreshed = stat.ModTime()
	}
	return &MaxMindGeoIP{
		reader:      reader,
		dbPath:      dbPath,
		refreshedAt: refreshed,
	}
}

// Lookup queries the underlying mmdb for ip and returns a *GeoLocation
// stamped with the lookup time. Returns (nil, nil) if the IP is not present
// in any indexed network — that's "no data" rather than an error so signals
// can treat it as "missing geo, do not trigger".
func (m *MaxMindGeoIP) Lookup(ip net.IP) (*GeoLocation, error) {
	if ip == nil {
		return nil, errors.New("risk geoip: nil ip")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.reader == nil {
		return nil, errors.New("risk geoip: closed")
	}
	// maxminddb-golang/v2 takes netip.Addr.
	addr, ok := netip.AddrFromSlice(ip.To16())
	if !ok {
		return nil, errors.New("risk geoip: invalid ip")
	}
	addr = addr.Unmap() // normalize 4-in-6 to v4 form

	result := m.reader.Lookup(addr)
	if err := result.Err(); err != nil {
		return nil, err
	}
	if !result.Found() {
		return nil, nil
	}
	var rec geoRecord
	if err := result.Decode(&rec); err != nil {
		return nil, err
	}
	return &GeoLocation{
		CountryCode: rec.Country.ISOCode,
		City:        rec.City.Names["en"],
		Lat:         rec.Location.Latitude,
		Lng:         rec.Location.Longitude,
		AccuracyKM:  int32(rec.Location.Accuracy),
		At:          time.Now(),
	}, nil
}

// geoRecord matches the GeoLite2-City / GeoIP2-City schema fields we care
// about. Unknown fields are silently skipped by the decoder.
type geoRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
		Accuracy  uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
}

// RefreshedAt reports the mtime of the underlying mmdb file at open time
// (best-effort; falls back to time.Now() on stat failure).
func (m *MaxMindGeoIP) RefreshedAt() time.Time { return m.refreshedAt }

// Healthy returns true when the reader is open and ready to serve lookups.
func (m *MaxMindGeoIP) Healthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reader != nil
}

// Close releases the underlying mmap'd reader. Safe to call multiple times.
func (m *MaxMindGeoIP) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reader == nil {
		return nil
	}
	err := m.reader.Close()
	m.reader = nil
	return err
}

// Path returns the path the database was opened from. Useful for diagnostics.
func (m *MaxMindGeoIP) Path() string { return m.dbPath }

// NoOpGeoIP is the air-gap / missing-DB fallback returned by NewMaxMindGeoIP
// when the file can't be opened. Lookup always returns (nil, nil) — geo
// signals MUST treat this as "no data" and not trigger.
type NoOpGeoIP struct{}

// Lookup always returns (nil, nil).
func (n *NoOpGeoIP) Lookup(_ net.IP) (*GeoLocation, error) { return nil, nil }

// RefreshedAt always returns the zero time.
func (n *NoOpGeoIP) RefreshedAt() time.Time { return time.Time{} }

// Healthy always returns false.
func (n *NoOpGeoIP) Healthy() bool { return false }
