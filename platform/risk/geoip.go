package risk

import (
	"net"
	"time"
)

// GeoIPLookup is the interface signals use to resolve IPs to geos.
//
// Full implementation arrives in this same file at Task 3 of TRD 09-03 —
// for now this declares the interface so risk.go can compile.
type GeoIPLookup interface {
	Lookup(ip net.IP) (*GeoLocation, error)
	RefreshedAt() time.Time
	Healthy() bool
}
