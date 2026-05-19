package risk

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeSignal is a deterministic Signal used to exercise the Evaluator.
type fakeSignal struct {
	name      string
	triggered bool
	weight    int32
	details   map[string]any
}

func (f *fakeSignal) Name() string { return f.name }
func (f *fakeSignal) Evaluate(_ context.Context, _ Request) (bool, int32, map[string]any) {
	return f.triggered, f.weight, f.details
}

func TestEvaluator_Empty_ZeroScore(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(nil)
	res := e.Eval(context.Background(), Request{})
	if res.Score != 0 {
		t.Fatalf("empty evaluator: want score 0, got %d", res.Score)
	}
	if len(res.Triggered) != 0 {
		t.Fatalf("empty evaluator: want zero triggered, got %d", len(res.Triggered))
	}
}

func TestEvaluator_SumsWeights(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "a", triggered: true, weight: 10},
		&fakeSignal{name: "b", triggered: false, weight: 50},
		&fakeSignal{name: "c", triggered: true, weight: 20},
	}
	e := NewEvaluator(signals)
	res := e.Eval(context.Background(), Request{})
	if res.Score != 30 {
		t.Fatalf("want score 30, got %d", res.Score)
	}
	if len(res.Triggered) != 2 {
		t.Fatalf("want 2 triggered, got %d", len(res.Triggered))
	}
	if res.Triggered[0].Signal != "a" || res.Triggered[1].Signal != "c" {
		t.Fatalf("want triggered=[a,c], got [%s,%s]", res.Triggered[0].Signal, res.Triggered[1].Signal)
	}
}

func TestEvaluator_ClipsAtMax(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "a", triggered: true, weight: 60},
		&fakeSignal{name: "b", triggered: true, weight: 60},
		&fakeSignal{name: "c", triggered: true, weight: 30},
	}
	e := NewEvaluator(signals)
	res := e.Eval(context.Background(), Request{})
	if res.Score != 100 {
		t.Fatalf("want score 100 (clipped), got %d", res.Score)
	}
	if len(res.Triggered) != 3 {
		t.Fatalf("want 3 triggered, got %d", len(res.Triggered))
	}
}

func TestEvaluator_CustomClip(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "a", triggered: true, weight: 40},
		&fakeSignal{name: "b", triggered: true, weight: 40},
	}
	e := NewEvaluator(signals, WithClip(50))
	res := e.Eval(context.Background(), Request{})
	if res.Score != 50 {
		t.Fatalf("want score 50, got %d", res.Score)
	}
}

func TestEvaluator_NegativeWeight_ClampsToZero(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "a", triggered: true, weight: -10},
	}
	e := NewEvaluator(signals)
	res := e.Eval(context.Background(), Request{})
	if res.Score != 0 {
		t.Fatalf("want score 0 (clamped), got %d", res.Score)
	}
}

func TestEvaluator_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "a", triggered: true, weight: 10},
		&fakeSignal{name: "b", triggered: true, weight: 20},
		&fakeSignal{name: "c", triggered: false, weight: 5},
	}
	e := NewEvaluator(signals)
	var wg sync.WaitGroup
	workers := runtime.GOMAXPROCS(0) * 4
	if workers < 16 {
		workers = 16
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				res := e.Eval(context.Background(), Request{
					TenantID:    uuid.New(),
					AccountID:   uuid.New(),
					AttemptedAt: time.Now(),
				})
				if res.Score != 30 {
					t.Errorf("concurrent eval got score %d, want 30", res.Score)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestEvaluator_NilRequest_NoPanic(t *testing.T) {
	t.Parallel()
	// Use a paranoid Signal that inspects every field.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Eval with zero Request panicked: %v", r)
		}
	}()
	signals := []Signal{&fakeSignal{name: "noop", triggered: false, weight: 0}}
	e := NewEvaluator(signals)
	_ = e.Eval(context.Background(), Request{})
}

func TestEvaluator_TriggeredDetailsCarried(t *testing.T) {
	t.Parallel()
	want := map[string]any{"reason": "test", "value": 42}
	signals := []Signal{&fakeSignal{name: "diag", triggered: true, weight: 5, details: want}}
	e := NewEvaluator(signals)
	res := e.Eval(context.Background(), Request{})
	if len(res.Triggered) != 1 {
		t.Fatalf("want 1 triggered, got %d", len(res.Triggered))
	}
	if res.Triggered[0].Details["reason"] != "test" || res.Triggered[0].Details["value"] != 42 {
		t.Fatalf("details not carried through: %+v", res.Triggered[0].Details)
	}
}

func TestEvaluator_AccessorsExposeConstruction(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "x", weight: 7},
		&fakeSignal{name: "y", weight: 9},
	}
	e := NewEvaluator(signals, WithClip(33))
	if got := e.ClipAt(); got != 33 {
		t.Fatalf("ClipAt: want 33, got %d", got)
	}
	if got := e.Signals(); len(got) != 2 {
		t.Fatalf("Signals(): want 2 entries, got %d", len(got))
	}
	if got := e.Signals()[0].Name(); got != "x" {
		t.Fatalf("Signals()[0]: want x, got %s", got)
	}
}

func TestEvaluator_WithClipZero_Unclipped(t *testing.T) {
	t.Parallel()
	// WithClip(0) disables the upper bound — surfaces an additive raw score
	// for diagnostic / tuning scenarios. Lower-bound clamp at 0 still applies.
	signals := []Signal{
		&fakeSignal{name: "a", triggered: true, weight: 80},
		&fakeSignal{name: "b", triggered: true, weight: 80},
	}
	e := NewEvaluator(signals, WithClip(0))
	res := e.Eval(context.Background(), Request{})
	if res.Score != 160 {
		t.Fatalf("WithClip(0) should be unclipped: want 160, got %d", res.Score)
	}
}

func TestEvaluator_TriggeredOrderMatchesSignalOrder(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		&fakeSignal{name: "first", triggered: true, weight: 1},
		&fakeSignal{name: "skipped", triggered: false, weight: 9},
		&fakeSignal{name: "third", triggered: true, weight: 2},
		&fakeSignal{name: "fourth", triggered: true, weight: 3},
	}
	e := NewEvaluator(signals)
	res := e.Eval(context.Background(), Request{})
	if len(res.Triggered) != 3 {
		t.Fatalf("want 3 triggered, got %d", len(res.Triggered))
	}
	wantNames := []string{"first", "third", "fourth"}
	for i, w := range wantNames {
		if res.Triggered[i].Signal != w {
			t.Fatalf("Triggered[%d]: want %s, got %s", i, w, res.Triggered[i].Signal)
		}
	}
}

func BenchmarkEvaluator_Eval_EightSignals(b *testing.B) {
	signals := []Signal{
		&fakeSignal{name: "s1", triggered: false, weight: 25},
		&fakeSignal{name: "s2", triggered: true, weight: 15},
		&fakeSignal{name: "s3", triggered: false, weight: 10},
		&fakeSignal{name: "s4", triggered: true, weight: 5},
		&fakeSignal{name: "s5", triggered: false, weight: 15},
		&fakeSignal{name: "s6", triggered: false, weight: 30},
		&fakeSignal{name: "s7", triggered: false, weight: 40},
		&fakeSignal{name: "s8", triggered: false, weight: 50},
	}
	e := NewEvaluator(signals)
	req := Request{
		TenantID:    uuid.New(),
		AccountID:   uuid.New(),
		AttemptedAt: time.Now(),
		SourceIP:    "203.0.113.5",
		UserAgent:   "Mozilla/5.0 (X11; Linux x86_64)",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Eval(context.Background(), req)
	}
}
