package rules

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ozy31/slo-as-code/internal/spec"
)

const day = 24 * time.Hour

func TestBurnRatesMatchTheSREWorkbook(t *testing.T) {
	want := []float64{14.4, 6, 3, 1}
	for i, w := range BurnRateWindows {
		if got := w.BurnRate(30 * day); math.Abs(got-want[i]) > 1e-9 {
			t.Errorf("row %d: burn rate %v, want %v", i, got, want[i])
		}
	}
}

func TestBurnRatesScaleWithWindow(t *testing.T) {
	// 2% of a 28-day budget in one hour is a 13.44x burn.
	if got := BurnRateWindows[0].BurnRate(28 * day); math.Abs(got-13.44) > 1e-9 {
		t.Fatalf("28d page burn rate = %v", got)
	}
}

func load(t *testing.T, path string) *spec.File {
	t.Helper()
	f, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCompileCheckout(t *testing.T) {
	rf := Compile(load(t, "../../examples/checkout.yaml"))
	if len(rf.Groups) != 4 {
		t.Fatalf("got %d groups, want 2 recording + 2 alerting", len(rf.Groups))
	}

	rec := rf.Groups[0]
	ratio := map[string]Rule{}
	for _, r := range rec.Rules {
		ratio[r.Record] = r
	}
	for _, w := range []string{"5m", "30m", "1h", "2h", "6h", "1d", "3d", "30d"} {
		r, ok := ratio["slo:sli_error:ratio_rate"+w]
		if !ok {
			t.Errorf("missing ratio for %s", w)
			continue
		}
		if !strings.Contains(r.Expr, "["+w+"]") || strings.Contains(r.Expr, spec.WindowPlaceholder) {
			t.Errorf("window %s not substituted: %s", w, r.Expr)
		}
		if r.Labels["service"] != "checkout" || r.Labels["slo"] != "availability" || r.Labels["team"] != "payments" {
			t.Errorf("labels for %s: %v", w, r.Labels)
		}
	}
	if got := ratio["slo:error_budget:ratio"].Expr; got != "0.001" {
		t.Errorf("error budget = %s", got)
	}

	alerts := rf.Groups[1].Rules
	var names []string
	for _, a := range alerts {
		names = append(names, a.Alert)
	}
	if strings.Join(names, ",") != "CheckoutAvailabilityBudgetBurnPage,CheckoutAvailabilityBudgetBurnTicket,CheckoutAvailabilityErrorBudgetExhausted" {
		t.Fatalf("unexpected alerts: %v", names)
	}
	exhausted := alerts[2]
	if exhausted.Expr != `slo:error_budget_remaining:ratio{service="checkout", slo="availability"} < 0` || exhausted.Labels["slo_alert"] != "exhausted" {
		t.Errorf("exhausted alert: %+v", exhausted)
	}
	page := alerts[0]
	for _, want := range []string{"ratio_rate1h", "ratio_rate5m", "> 0.0144", "ratio_rate6h", "ratio_rate30m", "> 0.006"} {
		if !strings.Contains(page.Expr, want) {
			t.Errorf("page expr missing %q:\n%s", want, page.Expr)
		}
	}
	if page.Labels["severity"] != "critical" || page.Annotations["runbook_url"] == "" {
		t.Errorf("page labels/annotations: %v %v", page.Labels, page.Annotations)
	}
}

func TestUnconfiguredClassesAreSkipped(t *testing.T) {
	rf := Compile(load(t, "../../examples/batch-ingest.yaml"))
	alerts := rf.Groups[1].Rules
	if len(alerts) != 1 || alerts[0].Labels["slo_alert"] != "ticket" {
		t.Fatalf("batch-ingest should only get a ticket alert, got %+v", alerts)
	}
}

func TestDisabledAlertingProducesNoGroup(t *testing.T) {
	f := load(t, "../../examples/batch-ingest.yaml")
	f.SLOs[0].Alerting.Disabled = true
	if got := len(Compile(f).Groups); got != 1 {
		t.Fatalf("got %d groups, want only the recording group", got)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{5 * time.Minute: "5m", 2 * time.Hour: "2h", 3 * day: "3d", 90 * time.Minute: "90m"}
	for in, want := range cases {
		if got := FormatDuration(in); got != want {
			t.Errorf("FormatDuration(%v) = %s, want %s", in, got, want)
		}
	}
}

func TestAlertAnnotationsFromSpec(t *testing.T) {
	f := &spec.File{
		Service: "search",
		SLOs: []spec.SLO{{
			Name:      "availability",
			Objective: 99.5,
			Window:    spec.Duration{Duration: 28 * 24 * time.Hour},
			SLI:       spec.SLI{Errors: "e[$window]", Total: "t[$window]"},
			Alerting: spec.Alerting{
				Annotations: map[string]string{"dashboard_url": "https://grafana.example/d/slo-search"},
				Page:        spec.Severity{Labels: map[string]string{"severity": "critical"}},
				Exhausted:   spec.Severity{Labels: map[string]string{"severity": "warning"}},
			},
		}},
	}
	alerts := Compile(f).Groups[1].Rules
	if len(alerts) != 2 {
		t.Fatalf("want page and exhausted alerts, got %d", len(alerts))
	}
	for _, a := range alerts {
		if a.Annotations["dashboard_url"] != "https://grafana.example/d/slo-search" || a.Annotations["summary"] == "" {
			t.Errorf("%s annotations: %v", a.Alert, a.Annotations)
		}
		if _, ok := a.Annotations["runbook_url"]; ok {
			t.Errorf("%s: runbook_url without a runbook", a.Alert)
		}
	}
}
