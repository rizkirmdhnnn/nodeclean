# nodeclean TUI Selector — Design

**Date:** 2026-05-30
**Status:** Approved (design), pending implementation plan

## Goal

Add an interactive terminal UI (TUI) to `nodeclean` so the user can pick *which*
discovered `node_modules` folders to delete, instead of the current all-or-nothing
`y/N` prompt. The TUI is the default interactive experience after a scan.

## Background

`nodeclean` is a single-file (~365 line) Go CLI with zero dependencies. It:
1. Scans the disk (or a path) for `node_modules` folders using concurrent walkers.
2. Sizes each folder in parallel.
3. Prints the list and a total.
4. Asks one `y/N` to delete **all** of them.

Limitation: no way to select a subset. This design adds a selector.

## Decisions (locked in)

| Decision | Choice |
|----------|--------|
| Trigger | TUI is the **default** interactive view after scanning. `-dry` and a new `-y` bypass it. |
| Features | Checkbox multi-select, select all/none, sort by size, live delete progress. |
| Library | Bubble Tea (`bubbletea` + `bubbles` + `lipgloss`). |
| Default checkbox state | **All unchecked** — user deliberately selects what to delete (prevents accidental mass-deletion). |

## Architecture

One cohesive Bubble Tea program with a four-state machine. The scan runs *inside*
the TUI as a `tea.Cmd` (with a spinner) rather than printing to stdout first — this
avoids competing terminal writes and flicker between the old escape-code progress
line and the TUI renderer.

```
scanning ──▶ selecting ──▶ deleting ──▶ done
                 │
                 └──(quit)──▶ exit (nothing deleted)
```

### States

1. **scanning** — spinner + "Scanning… N directories checked". Invokes the existing
   `concurrentWalk` + parallel `dirSize` logic from a `tea.Cmd`; results return as a
   `scanDoneMsg{targets []target}`.
2. **selecting** — scrollable checkbox list. All items start **unchecked**.
   - Keybindings:
     - `↑/↓`, `j/k` — move cursor
     - `space` — toggle current item
     - `a` — select all / none (toggle)
     - `s` — toggle sort order (size desc ↔ path asc)
     - `enter` — confirm and proceed to delete (no-op if nothing selected)
     - `q`, `esc`, `ctrl+c` — quit without deleting
   - Header: selected count + total selected size out of all.
   - Footer: keybinding hints.
3. **deleting** — deletes selected folders, streaming per-folder status
   (`✓ deleted <path> (1.2 GB)`) and a running freed-space total. Each deletion is a
   `tea.Cmd`; completion/error returns a message that advances the list.
4. **done** — summary: number removed, total space freed, and any per-folder errors.

### Non-TUI fallbacks (TUI is NOT shown)

| Condition | Behavior |
|-----------|----------|
| `-dry` | Print the discovered list + total as today, exit. No deletion. |
| `-y` (new flag) | Delete **all** discovered folders without prompting (script-friendly; the old non-interactive behavior). |
| stdout is not a TTY (piped/redirected) | Auto-fallback to plain-text list + plain delete, because a TUI cannot render. |

These paths reuse the existing plain-text rendering and deletion loop.

## File layout

Refactor the single `main.go` into focused files (no behavior change to scan logic):

- **`main.go`** — flag parsing, TTY detection, orchestration: decide scan→TUI vs.
  scan→plain-text based on flags and TTY.
- **`scan.go`** — `collectTargets`, `concurrentWalk`, `dirSize`, `skipDirs`,
  `excludeDirs`, `humanSize`, `target`/`options` types. Pure, no UI. The progress
  goroutine that printed `\rScanning…` is removed from here (the TUI owns progress;
  the plain-text path keeps a minimal version).
- **`tui.go`** — Bubble Tea `model`, the four states, `Update`, `View`, keybindings,
  styling via lipgloss.

New `go.mod` dependencies: `github.com/charmbracelet/bubbletea`,
`github.com/charmbracelet/bubbles`, `github.com/charmbracelet/lipgloss`.

## Flags (final set)

| Flag | Description |
|------|-------------|
| `-all` | Scan entire disk (default when no path given) |
| `-r` | Recursively scan subfolders |
| `-dry` | Print what would be deleted; no TUI, no deletion |
| `-y` | Delete all discovered folders without prompting; no TUI |

## Error handling

- Scan errors on individual dirs are ignored (as today — `WalkDir` returns `nil`).
- Delete errors are collected per-folder and shown in the `done` summary; one failure
  does not abort the rest.
- If the TUI fails to start (e.g. unexpected non-TTY despite detection), fall back to
  the plain-text path with an explanatory message.

## Testing

- **`scan.go`** — table tests: build a temp directory tree (including dirs that should
  be skipped/excluded), assert the discovered target set and sizes. Pure functions,
  no terminal needed.
- **`tui.go`** — drive `model.Update` with synthetic key/`scanDoneMsg`/delete messages
  and assert state transitions and selection state. Optionally use Bubble Tea's
  `teatest` for golden-output checks.

## Out of scope (YAGNI)

- Filtering/search within the list.
- Persisting selections between runs.
- Deleting things other than `node_modules`.
- Mouse support.
