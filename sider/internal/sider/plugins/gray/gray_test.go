package gray

import "testing"

func TestPickCanaryDeterministic(t *testing.T) {
	a := pickCanary("1.2.3.4", "s", 10)
	b := pickCanary("1.2.3.4", "s", 10)
	if a != b {
		t.Fatalf("expected deterministic pick, got %v vs %v", a, b)
	}
}

