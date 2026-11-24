package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDuplicateServiceIsRejected(t *testing.T) {
	src, err := os.ReadFile("../../examples/checkout.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "checkout-copy.yaml")
	if err := os.WriteFile(copyPath, src, 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{"../../examples/checkout.yaml", copyPath}

	for name, fn := range map[string]func() error{
		"validate":  func() error { return validate(paths) },
		"generate":  func() error { return generate(genOptions{outDir: filepath.Join(dir, "rules"), format: "rules"}, paths) },
		"dashboard": func() error { return dashboards(filepath.Join(dir, "dash"), paths) },
	} {
		err := fn()
		if err == nil || !strings.Contains(err.Error(), `service "checkout" is already defined`) {
			t.Errorf("%s: want duplicate service error, got %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "rules")); !os.IsNotExist(err) {
		t.Error("generate wrote output before rejecting the duplicate")
	}
}

func TestCheckDetectsStaleAndMissingOutput(t *testing.T) {
	dir := t.TempDir()
	paths := []string{"../../examples/checkout.yaml", "../../examples/batch-ingest.yaml"}

	if err := checkGenerated(genOptions{outDir: dir, format: "rules"}, paths); err == nil || !strings.Contains(err.Error(), "(missing)") {
		t.Fatalf("empty dir: want missing files, got %v", err)
	}
	if err := generate(genOptions{outDir: dir, format: "rules"}, paths); err != nil {
		t.Fatal(err)
	}
	if err := checkGenerated(genOptions{outDir: dir, format: "rules"}, paths); err != nil {
		t.Fatalf("freshly generated: %v", err)
	}

	target := filepath.Join(dir, "checkout.rules.yaml")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "0.0144", "0.02", 1)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	err = checkGenerated(genOptions{outDir: dir, format: "rules"}, paths)
	if err == nil || !strings.Contains(err.Error(), target) || strings.Contains(err.Error(), "batch-ingest") {
		t.Fatalf("hand-edited file: want only %s reported, got %v", target, err)
	}
}

func TestVersionPrefersLinkerValue(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	version = "v1.2.3"
	if got := buildVersion(); got != "v1.2.3" {
		t.Errorf("got %q", got)
	}
	version = ""
	if got := buildVersion(); got == "" {
		t.Error("empty version without ldflags")
	}
}

func TestPrometheusRuleNamespace(t *testing.T) {
	dir := t.TempDir()
	paths := []string{"../../examples/checkout.yaml"}
	if err := run([]string{"generate", "-o", dir, "-format", "prometheusrule", "-namespace", "monitoring", paths[0]}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "checkout.prometheusrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "namespace: monitoring") {
		t.Errorf("namespace missing:\n%s", data[:300])
	}
	if err := run([]string{"generate", "-o", dir, "-namespace", "monitoring", paths[0]}); err == nil {
		t.Error("-namespace with plain rules should be refused")
	}
}
