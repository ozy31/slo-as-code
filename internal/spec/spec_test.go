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
