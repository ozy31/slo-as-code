// Package spec defines the SLO file format and validates it.
package spec

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// WindowPlaceholder is replaced with each evaluation window ("5m", "1h", ...)
// when the SLI queries are expanded into recording rules.
const WindowPlaceholder = "$window"

// File is one service's SLO definition.
type File struct {
	Service string            `yaml:"service"`
	Labels  map[string]string `yaml:"labels,omitempty"`
	SLOs    []SLO             `yaml:"slos"`
}

// SLO is a single objective measured as a ratio of bad events to all events.
type SLO struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Objective   float64  `yaml:"objective"` // percent, e.g. 99.9
	Window      Duration `yaml:"window"`    // compliance period, e.g. 30d
	SLI         SLI      `yaml:"sli"`
	Alerting    Alerting `yaml:"alerting"`
}

// SLI holds PromQL for bad and total events. Both must contain $window.
// Instead of writing them by hand, a latency SLI can be declared with
// Latency and the queries are derived from the histogram.
type SLI struct {
	Errors  string   `yaml:"errors,omitempty"`
	Total   string   `yaml:"total,omitempty"`
	Latency *Latency `yaml:"latency,omitempty"`
}

// Latency describes "requests faster than Threshold are good" on a
// Prometheus histogram. Threshold must be one of the histogram's bucket
// boundaries, written exactly as the le label (e.g. "0.3").
type Latency struct {
	Metric    string `yaml:"metric"`    // histogram name without _bucket/_count
	Threshold string `yaml:"threshold"` // le value
	Selector  string `yaml:"selector,omitempty"`

	expanded bool
}

// queries expands a latency SLI into errors/total PromQL: slow requests are
// all requests minus those in the threshold bucket.
func (l Latency) queries() (errs, total string) {
	sel := strings.TrimSpace(l.Selector)
	join := func(extra string) string {
		switch {
		case sel == "":
			return "{" + extra + "}"
		case extra == "":
			return "{" + sel + "}"
		default:
			return "{" + sel + ", " + extra + "}"
		}
	}
	count := fmt.Sprintf("sum(rate(%s_count%s[%s]))", l.Metric, join(""), WindowPlaceholder)
	bucket := fmt.Sprintf(`sum(rate(%s_bucket%s[%s]))`, l.Metric, join(fmt.Sprintf(`le="%s"`, l.Threshold)), WindowPlaceholder)
	return count + " - " + bucket, count
}

// Alerting configures the generated burn-rate alerts.
type Alerting struct {
	Disabled bool   `yaml:"disabled,omitempty"`
	Runbook  string `yaml:"runbook,omitempty"`
	// Annotations are added to every alert of this SLO, e.g. dashboard_url.
	Annotations map[string]string `yaml:"annotations,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Page        Severity          `yaml:"page,omitempty"`
	Ticket      Severity          `yaml:"ticket,omitempty"`
	// Exhausted fires once the whole budget for the window is spent, i.e.
	// the SLO is already missed. Burn-rate alerts say it is about to happen.
	Exhausted Severity `yaml:"exhausted,omitempty"`
}

// Severity is the label set attached to one class of alert.
type Severity struct {
	Labels map[string]string `yaml:"labels,omitempty"`
}

// Duration accepts Prometheus-style durations including days ("30d").
type Duration struct{ time.Duration }

var durationRe = regexp.MustCompile(`^([0-9]+)(m|h|d|w)$`)

// UnmarshalYAML parses values like "28d", "4w" or "720h".
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	parsed, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

// ParseDuration parses a Prometheus-style duration with a single unit.
func ParseDuration(s string) (time.Duration, error) {
	m := durationRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 30d, 4w, 720h)", s)
	}
	var n int64
	fmt.Sscanf(m[1], "%d", &n)
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
	return time.Duration(n) * unit, nil
}

// Load reads and validates a file. Unknown keys are rejected so a typo in
// a field name fails loudly instead of silently dropping configuration.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.applyDefaults()
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

func (f *File) applyDefaults() {
	for i := range f.SLOs {
		s := &f.SLOs[i]
		if s.Window.Duration == 0 {
			s.Window.Duration = 30 * 24 * time.Hour
		}
		// Expand only when the hand-written queries are absent; Validate
		// reports the conflict if both were given.
		if s.SLI.Latency != nil && s.SLI.Errors == "" && s.SLI.Total == "" {
			s.SLI.Errors, s.SLI.Total = s.SLI.Latency.queries()
			s.SLI.Latency.expanded = true
		}
	}
}

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	metricRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelRe  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// reservedLabels are set by slogen. Letting a spec override them would
// break the selectors every generated rule uses, or alert routing.
var reservedLabels = map[string]bool{"service": true, "slo": true, "slo_alert": true}

func checkLabels(at string, labels map[string]string) []error {
	var errs []error
	for _, k := range sortedKeys(labels) {
		switch {
		case reservedLabels[k]:
			errs = append(errs, fmt.Errorf("%s: label %q is set by slogen and cannot be overridden", at, k))
		case !labelRe.MatchString(k) || strings.HasPrefix(k, "__"):
			errs = append(errs, fmt.Errorf("%s: %q is not a valid Prometheus label name", at, k))
		}
	}
	return errs
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Validate reports every problem in the file, not just the first.
func (f *File) Validate() error {
	var errs []error
	if !nameRe.MatchString(f.Service) {
		errs = append(errs, fmt.Errorf("service %q must match %s", f.Service, nameRe))
	}
	if len(f.SLOs) == 0 {
		errs = append(errs, errors.New("at least one SLO is required"))
	}
	errs = append(errs, checkLabels("labels", f.Labels)...)
	seen := map[string]bool{}
	for i, s := range f.SLOs {
		at := fmt.Sprintf("slos[%d] (%s)", i, s.Name)
		if !nameRe.MatchString(s.Name) {
			errs = append(errs, fmt.Errorf("%s: name must match %s", at, nameRe))
		}
		if seen[s.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate name", at))
		}
		seen[s.Name] = true
		if s.Objective <= 0 || s.Objective >= 100 {
			errs = append(errs, fmt.Errorf("%s: objective %.4g must be between 0 and 100 (exclusive)", at, s.Objective))
		}
		if s.Window.Duration < 24*time.Hour {
			errs = append(errs, fmt.Errorf("%s: window must be at least 1d", at))
		}
		errs = append(errs, checkLabels(at+" alerting.labels", s.Alerting.Labels)...)
		errs = append(errs, checkLabels(at+" alerting.page.labels", s.Alerting.Page.Labels)...)
		errs = append(errs, checkLabels(at+" alerting.ticket.labels", s.Alerting.Ticket.Labels)...)
		errs = append(errs, checkLabels(at+" alerting.exhausted.labels", s.Alerting.Exhausted.Labels)...)
		for _, k := range sortedKeys(s.Alerting.Annotations) {
			switch k {
			case "summary", "description", "runbook_url":
				errs = append(errs, fmt.Errorf("%s: alerting.annotations.%s is generated; use alerting.runbook for the runbook", at, k))
			}
		}
		if l := s.SLI.Latency; l != nil {
			if !l.expanded {
				errs = append(errs, fmt.Errorf("%s: use either sli.latency or sli.errors/sli.total, not both", at))
			}
			if !metricRe.MatchString(l.Metric) || strings.HasSuffix(l.Metric, "_bucket") || strings.HasSuffix(l.Metric, "_count") {
				errs = append(errs, fmt.Errorf("%s: sli.latency.metric %q must be a histogram name without _bucket or _count", at, l.Metric))
			}
			if _, err := strconv.ParseFloat(l.Threshold, 64); err != nil {
				errs = append(errs, fmt.Errorf("%s: sli.latency.threshold %q must be a bucket boundary such as \"0.3\"", at, l.Threshold))
			}
		}
		for field, q := range map[string]string{"sli.errors": s.SLI.Errors, "sli.total": s.SLI.Total} {
			if strings.TrimSpace(q) == "" {
				errs = append(errs, fmt.Errorf("%s: %s is required", at, field))
			} else if !strings.Contains(q, WindowPlaceholder) {
				errs = append(errs, fmt.Errorf("%s: %s must use %s as the range", at, field, WindowPlaceholder))
			}
		}
	}
	return errors.Join(errs...)
}

// ErrorBudget is the allowed bad-event ratio, e.g. 0.001 for 99.9%.
func (s SLO) ErrorBudget() float64 { return 1 - s.Objective/100 }
