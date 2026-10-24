package main

import (
	"os"
	"strings"
	"testing"
)

func TestNoDependencies(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "require") {
			t.Fatalf("go.mod must not require anything: %q", line)
		}
	}
}
