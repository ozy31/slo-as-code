package spec

import (
	"strings"
	"testing"
	"time"
)

func TestLoadExamples(t *testing.T) {
	for _, p := range []string{"../../examples/checkout.yaml", "../../examples/batch-ingest.yaml"} {
		if _, err := Load(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	_, err := Load("testdata/invalid.yaml")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		`service "Bad_Name"`,
		"objective 100 must be between 0 and 100",
		"window must be at least 1d",
		"sli.errors must use $window",
		"sli.total is required",
		"duplicate name",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	_, err := Load("testdata/typo.yaml")
	if err == nil || !strings.Contains(err.Error(), "windw") {
		t.Fatalf("expected an unknown field error for windw, got %v", err)
	}
}

func TestDefaultWindowIs30Days(t *testing.T) {
	file := &File{SLOs: []SLO{{}}}
	file.applyDefaults()
	if got := file.SLOs[0].Window.Duration; got != 30*24*time.Hour {
		t.Fatalf("default window = %v", got)
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"28d": 28 * 24 * time.Hour, "4w": 28 * 24 * time.Hour, "720h": 720 * time.Hour, "90m": 90 * time.Minute,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "30", "1y", "1h30m", "-1d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should fail", bad)
		}
	}
}

func TestLatencyShorthandExpands(t *testing.T) {
	f, err := Load("../../examples/checkout.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var lat SLO
	for _, s := range f.SLOs {
		if s.Name == "latency" {
			lat = s
		}
	}
	wantErrors := `sum(rate(http_request_duration_seconds_count{job="checkout"}[$window])) - ` +
		`sum(rate(http_request_duration_seconds_bucket{job="checkout", le="0.3"}[$window]))`
	if lat.SLI.Errors != wantErrors {
		t.Errorf("errors =\n%s\nwant\n%s", lat.SLI.Errors, wantErrors)
	}
	if lat.SLI.Total != `sum(rate(http_request_duration_seconds_count{job="checkout"}[$window]))` {
		t.Errorf("total = %s", lat.SLI.Total)
	}
}

func TestLatencyWithoutSelector(t *testing.T) {
	errs, total := Latency{Metric: "rpc_seconds", Threshold: "1"}.queries()
	if !strings.Contains(errs, `rpc_seconds_bucket{le="1"}`) || total != "sum(rate(rpc_seconds_count{}[$window]))" {
		t.Errorf("unexpected queries: %s | %s", errs, total)
	}
}

func TestLatencyConflictsAndBadValues(t *testing.T) {
	_, err := Load("testdata/latency-conflict.yaml")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"not both", "without _bucket or _count", `threshold "fast"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}
