package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// AuthMetrics holds Prometheus counters for authentication events.
type AuthMetrics struct {
	loginsTotal        *prometheus.CounterVec
	signupsTotal       *prometheus.CounterVec
	tokenRefreshesTotal *prometheus.CounterVec
}

// NewAuthMetrics creates auth-specific Prometheus counters and registers
// them with the provided registerer.
func NewAuthMetrics(reg prometheus.Registerer) *AuthMetrics {
	m := &AuthMetrics{
		loginsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "auth_logins_total",
				Help: "Total number of login attempts by result.",
			},
			[]string{"result"},
		),
		signupsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "auth_signups_total",
				Help: "Total number of signup attempts by result.",
			},
			[]string{"result"},
		),
		tokenRefreshesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "auth_token_refreshes_total",
				Help: "Total number of token refresh attempts by result.",
			},
			[]string{"result"},
		),
	}

	reg.MustRegister(m.loginsTotal)
	reg.MustRegister(m.signupsTotal)
	reg.MustRegister(m.tokenRefreshesTotal)

	return m
}

// RecordLogin increments the login counter.
func (m *AuthMetrics) RecordLogin(success bool) {
	m.loginsTotal.WithLabelValues(resultLabel(success)).Inc()
}

// RecordSignup increments the signup counter.
func (m *AuthMetrics) RecordSignup(success bool) {
	m.signupsTotal.WithLabelValues(resultLabel(success)).Inc()
}

// RecordTokenRefresh increments the token refresh counter.
func (m *AuthMetrics) RecordTokenRefresh(success bool) {
	m.tokenRefreshesTotal.WithLabelValues(resultLabel(success)).Inc()
}

func resultLabel(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}
