// nodeclean is a small CLI tool to find and remove node_modules folders,
// freeing up disk space in JavaScript/TypeScript projects.
//
// Usage:
//
//	go build -o nodeclean .
//	./nodeclean                        # scan entire disk, then pick folders in the TUI
//	./nodeclean <path>                 # clean node_modules in a single project folder
//	./nodeclean -r <path>              # recursively scan every node_modules under <path>
//	./nodeclean -dry                   # show what would be deleted without deleting
//	./nodeclean -y                     # delete everything found without prompting
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"time"
)

func main() {
	opts := parseFlags()

	info, err := os.Stat(opts.root)
	if err != nil {
		exitf("cannot access %q: %v\n", opts.root, err)
	}
	if !info.IsDir() {
		exitf("%q is not a directory\n", opts.root)
	}

	// Interactive TUI is the default. Fall back to plain text when the user
	// asked for a non-interactive mode (-dry / -y) or when stdout is not a
	// terminal (piped or redirected), where a TUI cannot render.
	if opts.dry || opts.yes || !isTTY() {
		runPlain(opts)
		return
	}

	runTUI(opts)
}

func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.recursive, "r", false, "recursively scan subfolders for node_modules")
	flag.BoolVar(&opts.all, "all", false, "scan entire disk for node_modules folders")
	flag.BoolVar(&opts.dry, "dry", false, "print what would be deleted without deleting")
	flag.BoolVar(&opts.yes, "y", false, "delete everything found without prompting (no TUI)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: nodeclean [flags] [path]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Without a path, scans the entire disk and opens an interactive selector.")
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

// isTTY reports whether stdout is connected to an interactive terminal.
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// runPlain handles the non-interactive paths: -dry, -y, and non-TTY output.
func runPlain(opts options) {
	if opts.all {
		fmt.Println("Scanning entire disk for node_modules folders...")
	}

	start := time.Now()
	var scanned atomic.Int64
	targets := collectTargets(opts, &scanned)
	if len(targets) == 0 {
		fmt.Println("Nothing to clean. No node_modules folders found.")
		return
	}

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].path < targets[j].path
	})

	var totalSize int64
	fmt.Printf("\nFound %d node_modules folder(s) in %s:\n\n",
		len(targets), time.Since(start).Round(time.Millisecond))
	for _, t := range targets {
		totalSize += t.size
		fmt.Printf("  %s (%s)\n", t.path, humanSize(t.size))
	}
	fmt.Println()
	fmt.Printf("Total: %d folder(s), %s\n\n", len(targets), humanSize(totalSize))

	if opts.dry {
		fmt.Println("Dry run complete. No files were deleted.")
		return
	}

	if !opts.yes {
		// Non-interactive terminal and the user did not pass -y: refuse to
		// delete without explicit consent.
		fmt.Println("Non-interactive output detected. Re-run with -y to delete all, or -dry to preview.")
		return
	}

	var totalDeleted int
	var freed int64
	for _, t := range targets {
		delStart := time.Now()
		if err := os.RemoveAll(t.path); err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %s -> %v\n", t.path, err)
			continue
		}
		totalDeleted++
		freed += t.size
		fmt.Printf("deleted %s (%s) in %s\n", t.path, humanSize(t.size),
			time.Since(delStart).Round(time.Millisecond))
	}

	fmt.Println("------")
	fmt.Printf("Done: removed %d folder(s), freed ~%s.\n", totalDeleted, humanSize(freed))
}
