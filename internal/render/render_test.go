package render

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is a fake renderer. It echoes its environment and body,
// records each run in $SWAG_ROOT/runs, and sleeps on a body of "SLEEP".
func TestHelperProcess(t *testing.T) {
	if !slices.Contains(os.Args, "--") {
		return
	}
	body, _ := io.ReadAll(os.Stdin)
	if string(body) == "SLEEP" {
		time.Sleep(10 * time.Second)
	}
	f, _ := os.OpenFile(filepath.Join(os.Getenv("SWAG_ROOT"), "runs"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	fmt.Fprintln(f, os.Getenv("SWAG_SOURCE"))
	f.Close()
	fmt.Printf("%s %s %s", os.Getenv("SWAG_SOURCE"), os.Getenv("SWAG_DRAFTS"), body)
	os.Exit(0)
}

var fake = []string{os.Args[0], "-test.run=^TestHelperProcess$", "--"}

func runs(t *testing.T, root string) int {
	data, _ := os.ReadFile(filepath.Join(root, "runs"))
	return strings.Count(string(data), "\n")
}

func TestRunOrderAndEnv(t *testing.T) {
	root := t.TempDir()
	var jobs []Job
	for i := range 20 {
		jobs = append(jobs, Job{Source: fmt.Sprintf("content/%02d.md", i), Argv: fake, Body: []byte(fmt.Sprint(i))})
	}
	results := Run(jobs, Options{Root: root, Drafts: true, Timeout: 10 * time.Second})
	for i, r := range results {
		if want := fmt.Sprintf("content/%02d.md 1 %d", i, i); string(r.HTML) != want || len(r.Diags) != 0 {
			t.Errorf("result %d = %q %v, want %q", i, r.HTML, r.Diags, want)
		}
	}
}

func TestCache(t *testing.T) {
	root := t.TempDir()
	o := Options{Root: root, Timeout: 10 * time.Second, Cache: NewCache()}
	jobs := []Job{{Source: "content/a.md", Argv: fake, Body: []byte("a")}, {Source: "content/b.md", Argv: fake, Body: []byte("b")}}
	Run(jobs, o)
	jobs[1].Body = []byte("b edited")
	results := Run(jobs, o)
	if n := runs(t, root); n != 3 {
		t.Errorf("renderer ran %d times, want 3 (only the edited body re-renders)", n)
	}
	if string(results[0].HTML) != "content/a.md 0 a" || string(results[1].HTML) != "content/b.md 0 b edited" {
		t.Errorf("results = %q %q", results[0].HTML, results[1].HTML)
	}
}

func TestTimeout(t *testing.T) {
	start := time.Now()
	r := Run([]Job{{Source: "content/slow.md", Argv: fake, Body: []byte("SLEEP")}}, Options{Root: t.TempDir(), Timeout: 200 * time.Millisecond})
	if len(r[0].Diags) != 1 || !strings.Contains(r[0].Diags[0].Msg, "timed out after 200ms") {
		t.Errorf("diagnostics = %v", r[0].Diags)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("timeout did not kill the renderer")
	}
}
