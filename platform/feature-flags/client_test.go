package featureflags

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestClient_BooleanFlag(t *testing.T) {
	src := NewMemorySourceWithFlags(
		Flag{Key: "on", Enabled: true},
		Flag{Key: "off", Enabled: false},
	)
	c := New(src)
	ctx := context.Background()

	if !c.IsEnabled(ctx, "on", Eval{}) {
		t.Error("expected on=true")
	}
	if c.IsEnabled(ctx, "off", Eval{}) {
		t.Error("expected off=false")
	}
	if c.IsEnabled(ctx, "missing", Eval{}) {
		t.Error("expected missing=false (closed)")
	}
}

func TestClient_VariantFlag(t *testing.T) {
	src := NewMemorySourceWithFlags(Flag{
		Key:     "theme",
		Enabled: true,
		Variants: map[string]any{
			"light": "light-theme",
			"dark":  "dark-theme",
			"hi":    "hi-contrast",
		},
		Default: "light",
	})
	c := New(src)
	ctx := context.Background()

	name, val, ok := c.Variant(ctx, "theme", Eval{})
	if !ok {
		t.Fatal("expected variant resolved")
	}
	if name != "light" {
		t.Errorf("name=%q want light", name)
	}
	if val != "light-theme" {
		t.Errorf("val=%v want light-theme", val)
	}

	// IsEnabled on a variant flag returns true when default fires.
	if !c.IsEnabled(ctx, "theme", Eval{}) {
		t.Error("variant flag with default should report enabled")
	}
}

func TestClient_VariantFlag_DisabledReturnsFalse(t *testing.T) {
	src := NewMemorySourceWithFlags(Flag{
		Key:      "theme",
		Enabled:  false,
		Variants: map[string]any{"light": 1, "dark": 2},
		Default:  "light",
	})
	c := New(src)
	if _, _, ok := c.Variant(context.Background(), "theme", Eval{}); ok {
		t.Error("disabled variant flag should not resolve")
	}
}

func TestClient_OverridePrecedence(t *testing.T) {
	// Subject > Household > Tenant > Environment.
	flag := Flag{
		Key:     "rollout",
		Enabled: true,
		Overrides: []Override{
			{Environment: "prod", Value: false},
			{TenantID: "t1", Value: true},
			{HouseholdID: "h1", Value: false},
			{SubjectID: "s1", Value: true},
		},
	}
	src := NewMemorySourceWithFlags(flag)
	c := New(src)
	ctx := context.Background()

	// Subject override wins over all others.
	if !c.IsEnabled(ctx, "rollout", Eval{
		SubjectID: "s1", HouseholdID: "h1", TenantID: "t1", Environment: "prod",
	}) {
		t.Error("subject override should win")
	}
	// Household > Tenant > Env.
	if c.IsEnabled(ctx, "rollout", Eval{
		HouseholdID: "h1", TenantID: "t1", Environment: "prod",
	}) {
		t.Error("household override (false) should win over tenant")
	}
	// Tenant > Env.
	if !c.IsEnabled(ctx, "rollout", Eval{TenantID: "t1", Environment: "prod"}) {
		t.Error("tenant override (true) should win over env")
	}
	// Env only.
	if c.IsEnabled(ctx, "rollout", Eval{Environment: "prod"}) {
		t.Error("env override (false) should apply")
	}
	// Nothing matches → fall through to flag.Enabled (true).
	if !c.IsEnabled(ctx, "rollout", Eval{Environment: "dev"}) {
		t.Error("no override → master enabled should apply")
	}
}

func TestClient_OverrideOrderTieBreak(t *testing.T) {
	// Two overrides at the same specificity: earlier-declared wins.
	flag := Flag{
		Key:     "k",
		Enabled: true,
		Overrides: []Override{
			{TenantID: "t1", Value: true},
			{TenantID: "t1", Value: false},
		},
	}
	c := New(NewMemorySourceWithFlags(flag))
	if !c.IsEnabled(context.Background(), "k", Eval{TenantID: "t1"}) {
		t.Error("first declared override should win on tie")
	}
}

func TestClient_PercentageRolloutDeterministic(t *testing.T) {
	flag := Flag{
		Key:     "exp",
		Enabled: true,
		Rollout: &Rollout{Percentage: 50, Salt: "exp"},
	}
	c := New(NewMemorySourceWithFlags(flag))
	ctx := context.Background()

	// Same subject + salt should always produce the same result across calls.
	for _, sub := range []string{"alice", "bob", "carol", "dan"} {
		first := c.IsEnabled(ctx, "exp", Eval{SubjectID: sub})
		for i := 0; i < 100; i++ {
			if got := c.IsEnabled(ctx, "exp", Eval{SubjectID: sub}); got != first {
				t.Fatalf("non-deterministic: subject=%s call=%d first=%v got=%v", sub, i, first, got)
			}
		}
	}
}

func TestClient_PercentageRolloutDistribution(t *testing.T) {
	flag := Flag{
		Key:     "exp",
		Enabled: true,
		Rollout: &Rollout{Percentage: 50, Salt: "exp-v1"},
	}
	c := New(NewMemorySourceWithFlags(flag))
	ctx := context.Background()
	hits := 0
	const n = 5000
	for i := 0; i < n; i++ {
		// Synthetic subject IDs.
		if c.IsEnabled(ctx, "exp", Eval{SubjectID: subjectID(i)}) {
			hits++
		}
	}
	// Expect ~2500. Allow generous variance because the bucket modulo-100 is
	// not perfectly uniform on a finite sample.
	if hits < 2200 || hits > 2800 {
		t.Errorf("rollout distribution off: hits=%d/%d (expected ~%d)", hits, n, n/2)
	}
}

func TestClient_RolloutZeroAndHundred(t *testing.T) {
	flag := func(p int) Flag {
		return Flag{
			Key:     "k",
			Enabled: true,
			Rollout: &Rollout{Percentage: p, Salt: "s"},
		}
	}
	c0 := New(NewMemorySourceWithFlags(flag(0)))
	c100 := New(NewMemorySourceWithFlags(flag(100)))
	ctx := context.Background()
	for _, sub := range []string{"a", "b", "c"} {
		if c0.IsEnabled(ctx, "k", Eval{SubjectID: sub}) {
			t.Errorf("0%% rollout should always be off (subject=%s)", sub)
		}
		if !c100.IsEnabled(ctx, "k", Eval{SubjectID: sub}) {
			t.Errorf("100%% rollout should always be on (subject=%s)", sub)
		}
	}
}

func TestClient_RolloutDifferentSaltsDiverge(t *testing.T) {
	mk := func(salt string) *Client {
		return New(NewMemorySourceWithFlags(Flag{
			Key:     "k",
			Enabled: true,
			Rollout: &Rollout{Percentage: 50, Salt: salt},
		}))
	}
	a := mk("salt-a")
	b := mk("salt-b")
	ctx := context.Background()

	// Across 200 subjects, the two salts should disagree on at least 25%
	// of subjects (otherwise the salts aren't actually diversifying).
	disagreements := 0
	const n = 200
	for i := 0; i < n; i++ {
		sub := subjectID(i)
		if a.IsEnabled(ctx, "k", Eval{SubjectID: sub}) != b.IsEnabled(ctx, "k", Eval{SubjectID: sub}) {
			disagreements++
		}
	}
	if disagreements < n/4 {
		t.Errorf("salts produced too-similar buckets: disagreements=%d/%d", disagreements, n)
	}
}

func TestClient_DefaultClosedOnSourceError(t *testing.T) {
	src := errSource{}
	var seenKey string
	var seenErr error
	c := New(src).WithErrorLogger(func(k string, err error) {
		seenKey = k
		seenErr = err
	})
	if c.IsEnabled(context.Background(), "anything", Eval{}) {
		t.Error("source error should default to false")
	}
	if seenKey != "anything" {
		t.Errorf("error logger key=%q", seenKey)
	}
	if !errors.Is(seenErr, ErrSourceUnavailable) {
		t.Errorf("error logger err=%v", seenErr)
	}
}

func TestClient_VariantOverrideUsesNamedVariant(t *testing.T) {
	flag := Flag{
		Key:     "theme",
		Enabled: true,
		Variants: map[string]any{
			"light": 1,
			"dark":  2,
		},
		Default: "light",
		Overrides: []Override{
			{TenantID: "darkco", Value: "dark"},
			{TenantID: "broken", Value: "doesnotexist"},
		},
	}
	c := New(NewMemorySourceWithFlags(flag))
	ctx := context.Background()

	name, _, _ := c.Variant(ctx, "theme", Eval{TenantID: "darkco"})
	if name != "dark" {
		t.Errorf("got %q want dark", name)
	}
	// Override pointing at unknown variant falls back to default.
	name, _, _ = c.Variant(ctx, "theme", Eval{TenantID: "broken"})
	if name != "light" {
		t.Errorf("invalid override should fall back to default; got %q", name)
	}
}

func TestClient_NewPanicsOnNilSource(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil Source")
		}
	}()
	_ = New(nil)
}

func TestMemorySource_DeleteAndConcurrency(t *testing.T) {
	src := NewMemorySource()
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src.Set(Flag{Key: keyFor(i), Enabled: true})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		f, ok, err := src.Lookup(context.Background(), keyFor(i))
		if err != nil || !ok || !f.Enabled {
			t.Errorf("missing flag %d", i)
		}
	}

	// Delete is concurrent-safe.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src.Delete(keyFor(i))
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if _, ok, _ := src.Lookup(context.Background(), keyFor(i)); ok {
			t.Errorf("flag %d should be deleted", i)
		}
	}
}

func subjectID(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, 8)
	for j := range out {
		out[j] = alphabet[(i+j*7)%len(alphabet)]
	}
	return string(out)
}

func keyFor(i int) string {
	return "k_" + subjectID(i)
}

// errSource implements Source and always returns ErrSourceUnavailable.
type errSource struct{}

func (errSource) Lookup(_ context.Context, _ string) (Flag, bool, error) {
	return Flag{}, false, ErrSourceUnavailable
}
