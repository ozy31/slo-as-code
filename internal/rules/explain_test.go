package rules

import (
	"strings"
	"testing"
)

func TestExplainCheckout(t *testing.T) {
	var b strings.Builder
	if err := Explain(&b, load(t, "../../examples/checkout.yaml")); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"checkout / availability: 99.9% over 30d",
		"43m12s of full downtime", // 0.1% of 30 days
		"14.4x", "1.44%", "2.1d",  // 2% of the budget per hour -> gone in 50 hours
		"checkout / latency: 99% over 30d",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explain output missing %q:\n%s", want, out)
		}
	}
}

func TestExplainMarksUnconfiguredClasses(t *testing.T) {
	var b strings.Builder
	if err := Explain(&b, load(t, "../../examples/batch-ingest.yaml")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "page (off)") {
		t.Errorf("batch-ingest has no page alert, output:\n%s", b.String())
	}
}
