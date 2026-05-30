package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// Directories to skip during full-disk scan to avoid slow/irrelevant paths.
var skipDirs = map[string]bool{
	".Trash":       true,
	"Library":      true,
	"System":       true,
	"Applications": true,
	"proc":         true,
	"sys":          true,
	"dev":          true,
	".git":         true,
	".docker":      true,
	"vendor":       true,
	".cargo":       true,
	".rustup":      true,
	"go":           true,
	"opt":          true,
	"private":      true,
}

// Directories whose node_modules should NOT be deleted — these belong to
// IDE extensions, language servers, version managers, and other tools that
// need their own node_modules to function.
var excludeDirs = map[string]bool{
	".vscode":      true,
	".cursor":      true,
	".antigravity": true,
	".copilot":     true,
	".local":       true,
	".nvm":         true,
	".npm":         true,
	".cache":       true,
	".config":      true,
	".bun":         true,
	".pnpm":        true,
	".yarn":        true,
}

type options struct {
	root      string
	recursive bool
	dry       bool
	all       bool
	yes       bool
}

type target struct {
	path string
	size int64
}

// collectTargets finds node_modules folders and calculates their sizes concurrently.
// scanned, if non-nil, is incremented for every directory inspected so callers can
// display live progress.
func collectTargets(opts options, scanned *atomic.Int64) []target {
	if !opts.recursive {
		p := filepath.Join(opts.root, "node_modules")
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			size, _ := dirSize(abs)
			return []target{{path: abs, size: size}}
		}
		return nil
	}

	// Phase 1: concurrent walk to discover node_modules paths.
	paths := concurrentWalk(opts, scanned)
	if len(paths) == 0 {
		return nil
	}

	// Phase 2: calculate sizes in parallel.
	workers := min(runtime.NumCPU(), len(paths))

	targets := make([]target, len(paths))
	var wg sync.WaitGroup
	ch := make(chan int, len(paths))
	for i := range paths {
		ch <- i
	}
	close(ch)

	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range ch {
				size, _ := dirSize(paths[i])
				targets[i] = target{path: paths[i], size: size}
			}
		}()
	}
	wg.Wait()

	return targets
}

// concurrentWalk discovers node_modules directories using parallel workers.
// Top-level directories under root are distributed across goroutines.
func concurrentWalk(opts options, scanned *atomic.Int64) []string {
	entries, err := os.ReadDir(opts.root)
	if err != nil {
		return nil
	}

	var mu sync.Mutex
	var results []string

	bump := func() {
		if scanned != nil {
			scanned.Add(1)
		}
	}

	// Filter top-level dirs that we should walk.
	var walkDirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if opts.all && skipDirs[name] {
			continue
		}
		if excludeDirs[name] {
			continue
		}
		if name == "node_modules" {
			abs, _ := filepath.Abs(filepath.Join(opts.root, name))
			mu.Lock()
			results = append(results, abs)
			mu.Unlock()
			continue
		}
		walkDirs = append(walkDirs, filepath.Join(opts.root, name))
	}

	// Walk each top-level directory in its own goroutine, bounded by CPU count.
	workers := min(runtime.NumCPU(), len(walkDirs))

	var wg sync.WaitGroup
	ch := make(chan string, len(walkDirs))
	for _, d := range walkDirs {
		ch <- d
	}
	close(ch)

	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for dir := range ch {
				_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
					if err != nil || !d.IsDir() {
						return nil
					}
					bump()
					name := d.Name()

					if skipDirs[name] {
						return fs.SkipDir
					}
					if excludeDirs[name] {
						return fs.SkipDir
					}
					if name == "node_modules" {
						abs, _ := filepath.Abs(path)
						mu.Lock()
						results = append(results, abs)
						mu.Unlock()
						return fs.SkipDir
					}
					return nil
				})
			}
		}()
	}
	wg.Wait()

	return results
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size, err
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func exitf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}
