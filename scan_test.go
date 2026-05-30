package main

import (
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
)

// makeTree builds a directory tree under root. Each path ending in a file gets
// content written; directories are created as needed.
func makeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectTargetsRecursive(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"projA/node_modules/dep/index.js": "console.log(1)",
		"projB/sub/node_modules/x.js":     "x",
		".vscode/node_modules/ext.js":     "excluded", // excluded dir
		"projC/src/app.ts":                "no deps",  // no node_modules
	})

	var scanned atomic.Int64
	got := collectTargets(options{root: root, recursive: true}, &scanned)

	var paths []string
	for _, tg := range got {
		paths = append(paths, tg.path)
	}
	sort.Strings(paths)

	want := []string{
		filepath.Join(root, "projA/node_modules"),
		filepath.Join(root, "projB/sub/node_modules"),
	}
	if len(paths) != len(want) {
		t.Fatalf("found %d targets %v, want %d %v", len(paths), paths, len(want), want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("target[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
	if scanned.Load() == 0 {
		t.Error("expected scanned counter to be incremented")
	}
}

func TestCollectTargetsNonRecursive(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, map[string]string{
		"node_modules/a.js":     "a",
		"sub/node_modules/b.js": "b", // should NOT be found without -r
	})

	got := collectTargets(options{root: root, recursive: false}, nil)
	if len(got) != 1 {
		t.Fatalf("found %d targets, want 1: %v", len(got), got)
	}
	if got[0].path != filepath.Join(root, "node_modules") {
		t.Errorf("got %q", got[0].path)
	}
	if got[0].size == 0 {
		t.Error("expected non-zero size")
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
