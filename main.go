// nodeclean is a small CLI tool to find and remove node_modules folders,
// freeing up disk space in JavaScript/TypeScript projects.
//
// Usage:
//
//	go build -o nodeclean .
//	./nodeclean                        # scan entire disk (default)
//	./nodeclean <path>                 # clean node_modules in a single project folder
//	./nodeclean -r <path>              # recursively clean every node_modules under <path>
//	./nodeclean -dry                   # show what would be deleted without deleting
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
}

type target struct {
	path string
	size int64
}

func main() {
	opts := parseFlags()

	if opts.all {
		fmt.Println("Scanning entire disk for node_modules folders...")
	}

	info, err := os.Stat(opts.root)
	if err != nil {
		exitf("cannot access %q: %v\n", opts.root, err)
	}
	if !info.IsDir() {
		exitf("%q is not a directory\n", opts.root)
	}

	start := time.Now()
	targets := collectTargets(opts)
	if len(targets) == 0 {
		fmt.Println("Nothing to clean. No node_modules folders found.")
		return
	}

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].path < targets[j].path
	})

	// Print results.
	var totalSize int64
	fmt.Printf("\nFound %d node_modules folder(s) in %s:\n\n",
		len(targets), time.Since(start).Round(time.Millisecond))
	for _, t := range targets {
		totalSize += t.size
		fmt.Printf("  %s (%s)\n", t.path, humanSize(t.size))
	}
	fmt.Println()
	fmt.Printf("Total: %d folder(s), %s\n", len(targets), humanSize(totalSize))
	fmt.Println()

	if opts.dry {
		fmt.Println("Dry run complete. No files were deleted.")
		return
	}

	// Ask for confirmation before deleting.
	fmt.Print("Proceed with deletion? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Aborted.")
		return
	}

	fmt.Println()
	var totalDeleted int
	for _, t := range targets {
		delStart := time.Now()
		if err := os.RemoveAll(t.path); err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %s -> %v\n", t.path, err)
			continue
		}
		totalDeleted++
		fmt.Printf("deleted %s (%s) in %s\n", t.path, humanSize(t.size),
			time.Since(delStart).Round(time.Millisecond))
	}

	fmt.Println("------")
	fmt.Printf("Done: removed %d folder(s), freed ~%s.\n", totalDeleted, humanSize(totalSize))
}

func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.recursive, "r", false, "recursively scan subfolders for node_modules")
	flag.BoolVar(&opts.all, "all", false, "scan entire disk for node_modules folders")
	flag.BoolVar(&opts.dry, "dry", false, "print what would be deleted without deleting")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: nodeclean [flags] [path]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Without a path, scans the entire disk (same as -all).")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() >= 1 {
		opts.root = flag.Arg(0)
	} else {
		opts.all = true
		opts.recursive = true
		opts.root = "/"
	}

	if opts.all {
		opts.recursive = true
		opts.root = "/"
	}

	return opts
}

// collectTargets finds node_modules folders and calculates their sizes concurrently.
func collectTargets(opts options) []target {
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
	// Use a pool of walkers, each processing directories from a shared queue.
	paths := concurrentWalk(opts)

	if len(paths) == 0 {
		return nil
	}

	// Phase 2: calculate sizes in parallel.
	workers := runtime.NumCPU()
	if workers > len(paths) {
		workers = len(paths)
	}

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
func concurrentWalk(opts options) []string {
	// Read top-level entries to fan out work.
	entries, err := os.ReadDir(opts.root)
	if err != nil {
		return nil
	}

	var mu sync.Mutex
	var results []string
	var dirsScanned atomic.Int64

	// Show progress in a separate goroutine.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				fmt.Printf("\r\033[K") // clear progress line
				return
			case <-ticker.C:
				fmt.Printf("\rScanning... %d directories checked", dirsScanned.Load())
			}
		}
	}()

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
	workers := runtime.NumCPU()
	if workers > len(walkDirs) {
		workers = len(walkDirs)
	}

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
					dirsScanned.Add(1)
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
	close(done)

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
