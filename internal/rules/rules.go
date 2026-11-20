// Package rules compiles SLO specs into Prometheus rule groups.
package rules

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ozy31/slo-as-code/internal/spec"
)

// RuleFile is the Prometheus rule file format (also read by vmalert and
// the Prometheus Operator's PrometheusRule spec).
type RuleFile struct {
	Groups []Group `yaml:"groups"`
}

// Group is one rule group.
type Group struct {
	Name     string `yaml:"name"`
	Interval string `yaml:"interval,omitempty"`
	Rules    []Rule `yaml:"rules"`
}

// Rule is a recording or alerting rule.
type Rule struct {
	Record      string            `yaml:"record,omitempty"`
	Alert       string            `yaml:"alert,omitempty"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// SLIWindows are the short windows the burn-rate alerts need, in addition
// to the SLO's own compliance window.
var SLIWindows = []time.Duration{
	5 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour,
	6 * time.Hour, 24 * time.Hour, 3 * 24 * time.Hour,
}

// Compile turns a spec file into rule groups: one recording group per SLO
// and, unless disabled, one alerting group per SLO.
func Compile(f *spec.File) RuleFile {
	var out RuleFile
	for _, s := range f.SLOs {
		labels := sloLabels(f, s)
		out.Groups = append(out.Groups, recordingGroup(f, s, labels))
	}
	return out
}

func recordingGroup(f *spec.File, s spec.SLO, labels map[string]string) Group {
	g := Group{Name: fmt.Sprintf("slo:%s:%s:recording", f.Service, s.Name), Interval: "30s"}
	windows := append(slices.Clone(SLIWindows), s.Window.Duration)
	for _, w := range windows {
		g.Rules = append(g.Rules, Rule{
			Record: ratioMetric(w),
			Expr:   errorRatioExpr(s, w),
			Labels: labels,
		})
	}
	g.Rules = append(g.Rules,
		Rule{Record: "slo:objective:ratio", Expr: formatFloat(s.Objective / 100), Labels: labels},
		Rule{Record: "slo:error_budget:ratio", Expr: formatFloat(s.ErrorBudget()), Labels: labels},
	)
	return g
}

// ratioMetric names the recorded error ratio for a window, e.g.
// slo:sli_error:ratio_rate1h.
func ratioMetric(w time.Duration) string {
	return "slo:sli_error:ratio_rate" + FormatDuration(w)
}

func errorRatioExpr(s spec.SLO, w time.Duration) string {
	win := FormatDuration(w)
	errs := strings.ReplaceAll(strings.TrimSpace(s.SLI.Errors), spec.WindowPlaceholder, win)
	total := strings.ReplaceAll(strings.TrimSpace(s.SLI.Total), spec.WindowPlaceholder, win)
	return fmt.Sprintf("(%s)\n/\n(%s)", errs, total)
}

func sloLabels(f *spec.File, s spec.SLO) map[string]string {
	l := maps.Clone(f.Labels)
	if l == nil {
		l = map[string]string{}
	}
	l["service"] = f.Service
	l["slo"] = s.Name
	return l
}

// selector renders a label matcher that picks this SLO's series.
func selector(labels map[string]string) string {
	return fmt.Sprintf(`{service="%s", slo="%s"}`, labels["service"], labels["slo"])
}

// FormatDuration renders a duration in the largest whole Prometheus unit.
func FormatDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	default:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
}

func formatFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
}
