package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
)

func newTestManager(production bool) *Manager {
	return NewWithStore(Config{
		CookieName:   "_test_session",
		CookieDomain: "",
		Production:   production,
		Lifetime:     time.Hour,
	}, memstore.New())
}

func TestNewWithStore_DevelopmentCookieConfig(t *testing.T) {
	m := newTestManager(false)
	if m.Cookie.Name != "_test_session" {
		t.Errorf("cookie name: %q", m.Cookie.Name)
	}
	if m.Cookie.Secure {
		t.Error("expected Secure=false in development")
	}
	if m.Cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", m.Cookie.SameSite)
	}
	if !m.Cookie.HttpOnly {
		t.Error("expected HttpOnly=true")
	}
}

func TestNewWithStore_ProductionCookieConfig(t *testing.T) {
	m := newTestManager(true)
	if !m.Cookie.Secure {
		t.Error("expected Secure=true in production")
	}
	if m.Cookie.SameSite != http.SameSiteNoneMode {
		t.Errorf("expected SameSite=None, got %v", m.Cookie.SameSite)
	}
}

func TestNewWithStore_DefaultLifetime(t *testing.T) {
	m := NewWithStore(Config{CookieName: "x"}, memstore.New())
	if m.Lifetime != DefaultLifetime {
		t.Errorf("expected default lifetime %v, got %v", DefaultLifetime, m.Lifetime)
	}
}

func TestUserIDRoundtrip(t *testing.T) {
	m := newTestManager(false)
	handler := m.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if _, ok := m.GetUserID(ctx); ok {
			t.Error("expected no user initially")
		}
		m.SetUserID(ctx, "user-1")
		id, ok := m.GetUserID(ctx)
		if !ok || id != "user-1" {
			t.Errorf("got %q ok=%v", id, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)
}

func TestTwoFactorUserIDRoundtrip(t *testing.T) {
	m := newTestManager(false)
	handler := m.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		m.SetTwoFactorUserID(ctx, "u-2fa")
		id, ok := m.GetTwoFactorUserID(ctx)
		if !ok || id != "u-2fa" {
			t.Errorf("2fa: got %q ok=%v", id, ok)
		}
		if _, ok := m.GetUserID(ctx); ok {
			t.Error("regular user should be empty")
		}
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)
}

func TestClear(t *testing.T) {
	m := newTestManager(false)
	handler := m.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		m.SetUserID(ctx, "u-clear")
		if err := m.Clear(ctx); err != nil {
			t.Fatalf("clear: %v", err)
		}
		if _, ok := m.GetUserID(ctx); ok {
			t.Error("expected no user after clear")
		}
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)
}

func TestRecordMetadataNoPool_NoOp(t *testing.T) {
	m := newTestManager(false)
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	if err := m.RecordMetadata(context.Background(), req); err != nil {
		t.Errorf("expected no-op without pool, got %v", err)
	}
	// TouchActivity should also be a no-op (no panic).
	m.TouchActivity(context.Background())
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(r *http.Request)
		remoteAddr string
		want       string
	}{
		{
			name: "x-forwarded-for first hop",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
			},
			want: "1.2.3.4",
		},
		{
			name: "x-real-ip",
			setup: func(r *http.Request) {
				r.Header.Set("X-Real-IP", "9.8.7.6")
			},
			want: "9.8.7.6",
		},
		{
			name:       "remote addr port stripped",
			remoteAddr: "10.0.0.1:53121",
			want:       "10.0.0.1",
		},
		{
			name:       "remote addr no port",
			remoteAddr: "10.0.0.2",
			want:       "10.0.0.2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.setup != nil {
				tc.setup(req)
			}
			if got := ClientIP(req); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
