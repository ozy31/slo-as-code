// Command slogen compiles SLO specs into Prometheus rule files.
//
//	slogen generate -o out/rules examples/*.yaml   one rule file per spec
//	slogen generate -format prometheusrule ...     PrometheusRule CRs instead
//	slogen generate -check -o deploy/rules ...      fail if committed output is stale
//	slogen validate examples/*.yaml                 check specs only
//	slogen dashboard -o out/dashboards examples/*.yaml   Grafana JSON per spec
//	slogen explain examples/checkout.yaml           budgets and alert thresholds
//	slogen version                                  build version
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ozy31/slo-as-code/internal/dashboard"
	"github.com/ozy31/slo-as-code/internal/rules"
	"github.com/ozy31/slo-as-code/internal/spec"
)

const usage = `usage:
  slogen validate <spec.yaml>...
  slogen generate [-o dir] [-format rules|prometheusrule] [-check] <spec.yaml>...
  slogen dashboard [-o dir] <spec.yaml>...
  slogen explain <spec.yaml>...
  slogen version
`

// version is set by the release build (-ldflags "-X main.version=v1.2.3").
var version = ""

// buildVersion falls back to the module version for `go install ...@v1.2.3`
// and to "dev" for local builds.
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "validate":
		return validate(args[1:])
	case "version", "-version", "--version":
		fmt.Println("slogen", buildVersion())
		return nil
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		outDir := fs.String("o", "out/rules", "output directory")
		format := fs.String("format", "rules", "rules (plain Prometheus/vmalert file) or prometheusrule (Prometheus Operator CR)")
		check := fs.Bool("check", false, "write nothing; fail if files in -o differ from what would be generated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *format != "rules" && *format != "prometheusrule" {
			return fmt.Errorf("unknown format %q", *format)
		}
		if *check {
			return checkGenerated(*outDir, *format, fs.Args())
		}
		return generate(*outDir, *format, fs.Args())
	case "explain":
		if len(args) < 2 {
			return errors.New(usage)
		}
		for _, p := range args[1:] {
			f, err := spec.Load(p)
			if err != nil {
				return err
			}
			if err := rules.Explain(os.Stdout, f); err != nil {
				return err
			}
		}
		return nil
	case "dashboard":
		fs := flag.NewFlagSet("dashboard", flag.ContinueOnError)
		outDir := fs.String("o", "out/dashboards", "output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return dashboards(*outDir, fs.Args())
	default:
		return errors.New(usage)
	}
}

func dashboards(outDir string, paths []string) error {
	if len(paths) == 0 {
		return errors.New(usage)
	}
	files, err := loadAll(paths)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for i, f := range files {
		p := paths[i]
		out, err := dashboard.Render(f)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, f.Service+".json")
		if err := os.WriteFile(dst, append(out, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s -> %s\n", p, dst)
	}
	return nil
}

func validate(paths []string) error {
	if len(paths) == 0 {
		return errors.New(usage)
	}
	var errs []error
	seen := map[string]string{}
	for _, p := range paths {
		f, err := spec.Load(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, dup := seen[f.Service]; dup {
			errs = append(errs, duplicateService(f.Service, prev, p))
			continue
		}
		seen[f.Service] = p
		fmt.Printf("ok  %s\n", p)
	}
	return errors.Join(errs...)
}

// loadAll loads every spec and refuses two specs for the same service:
// output files are named after the service, so the second would silently
// overwrite the first.
func loadAll(paths []string) ([]*spec.File, error) {
	files := make([]*spec.File, 0, len(paths))
	seen := map[string]string{}
	for _, p := range paths {
		f, err := spec.Load(p)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[f.Service]; dup {
			return nil, duplicateService(f.Service, prev, p)
		}
		seen[f.Service] = p
		files = append(files, f)
	}
	return files, nil
}

func duplicateService(service, first, second string) error {
	return fmt.Errorf("%s: service %q is already defined in %s; put all of its SLOs in one file", second, service, first)
}

// output is one generated file: where it goes and what it contains.
type output struct {
	source, path string
	data         []byte
}

func render(outDir, format string, paths []string) ([]output, error) {
	if len(paths) == 0 {
		return nil, errors.New(usage)
	}
	files, err := loadAll(paths)
	if err != nil {
		return nil, err
	}
	outs := make([]output, 0, len(files))
	for i, f := range files {
		var doc any = rules.Compile(f)
		dst := filepath.Join(outDir, f.Service+".rules.yaml")
		if format == "prometheusrule" {
			doc = prometheusRule(f, rules.Compile(f))
			dst = filepath.Join(outDir, f.Service+".prometheusrule.yaml")
		}
		data, err := marshal(doc, paths[i])
		if err != nil {
			return nil, err
		}
		outs = append(outs, output{source: paths[i], path: dst, data: data})
	}
	return outs, nil
}

func generate(outDir, format string, paths []string) error {
	outs, err := render(outDir, format, paths)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, o := range outs {
		if err := os.WriteFile(o.path, o.data, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s -> %s\n", o.source, o.path)
	}
	return nil
}

// checkGenerated is for repositories that commit the generated rules: CI
// runs it so a spec change without regenerated output fails the build.
func checkGenerated(outDir, format string, paths []string) error {
	outs, err := render(outDir, format, paths)
	if err != nil {
		return err
	}
	var stale []string
	for _, o := range outs {
		current, err := os.ReadFile(o.path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			stale = append(stale, o.path+" (missing)")
		case err != nil:
			return err
		case !bytes.Equal(current, o.data):
			stale = append(stale, o.path)
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("generated rules are out of date, run slogen generate:\n  %s", strings.Join(stale, "\n  "))
	}
	fmt.Printf("%d generated files up to date\n", len(outs))
	return nil
}

// prometheusRule wraps the groups in a Prometheus Operator custom resource.
// The labels let an operator's ruleSelector pick up generated rules only.
func prometheusRule(f *spec.File, rf rules.RuleFile) map[string]any {
	return map[string]any{
		"apiVersion": "monitoring.coreos.com/v1",
		"kind":       "PrometheusRule",
		"metadata": map[string]any{
			"name": "slo-" + f.Service,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "slogen",
				"slo.service":                  f.Service,
			},
		},
		"spec": rf,
	}
}

func marshal(rf any, source string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by slogen from %s. Do not edit.\n", filepath.ToSlash(source))
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(rf); err != nil {
		return nil, err
	}
	return []byte(b.String()), enc.Close()
}
