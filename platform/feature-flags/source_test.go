package featureflags

import (
	"context"
	"testing"
)

func TestEnvSource_Lookup(t *testing.T) {
	tests := []struct {
		key      string
		envVar   string
		envValue string
		want     bool
		wantOK   bool
	}{
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "true", true, true},
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "1", true, true},
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "yes", true, true},
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "On", true, true},
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "false", false, true},
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "0", false, true},
		{"household_billing", "FEATURE_FLAGS_HOUSEHOLD_BILLING", "garbage", false, true},
		{"new-dashboard", "FEATURE_FLAGS_NEW_DASHBOARD", "true", true, true},
		{"k.with.dots", "FEATURE_FLAGS_K_WITH_DOTS", "true", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.envVar, func(t *testing.T) {
			s := &EnvSource{LookupFn: func(name string) (string, bool) {
				if name == tt.envVar {
					return tt.envValue, true
				}
				return "", false
			}}
			f, ok, err := s.Lookup(context.Background(), tt.key)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("ok=%v want %v", ok, tt.wantOK)
			}
			if f.Enabled != tt.want {
				t.Errorf("enabled=%v want %v", f.Enabled, tt.want)
			}
		})
	}
}

func TestEnvSource_Missing(t *testing.T) {
	s := &EnvSource{LookupFn: func(string) (string, bool) { return "", false }}
	_, ok, err := s.Lookup(context.Background(), "x")
	if err != nil {
		t.Errorf("err=%v", err)
	}
	if ok {
		t.Error("expected ok=false for missing env var")
	}
}

func TestEnvSource_CustomPrefix(t *testing.T) {
	s := &EnvSource{
		Prefix: "FF_",
		LookupFn: func(name string) (string, bool) {
			if name == "FF_X" {
				return "true", true
			}
			return "", false
		},
	}
	f, ok, err := s.Lookup(context.Background(), "x")
	if err != nil || !ok || !f.Enabled {
		t.Errorf("custom prefix: f=%v ok=%v err=%v", f, ok, err)
	}
}

func TestEnvify(t *testing.T) {
	cases := map[string]string{
		"household_billing": "HOUSEHOLD_BILLING",
		"new-dashboard":     "NEW_DASHBOARD",
		"a.b.c":             "A_B_C",
		"123abc":            "123ABC",
		"":                  "",
	}
	for in, want := range cases {
		if got := envify(in); got != want {
			t.Errorf("envify(%q)=%q want %q", in, got, want)
		}
	}
}
