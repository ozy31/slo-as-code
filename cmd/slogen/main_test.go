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
		"generate":  func() error { return generate(filepath.Join(dir, "rules"), "rules", paths) },
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
