# nodeclean

A fast CLI tool to find and remove `node_modules` folders from your disk, freeing up gigabytes of space — with an interactive TUI to pick exactly which ones to delete.

## Installation

### Quick install (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/rizkirmdhnnn/nodeclean/main/install.sh | sh
```

### With Go

```bash
go install github.com/rizkirmdhnnn/nodeclean@latest
```

### From source

```bash
git clone https://github.com/rizkirmdhnnn/nodeclean.git
cd nodeclean
go build -o nodeclean .
sudo mv nodeclean /usr/local/bin/
```

## Usage

```bash
# Scan entire disk, then pick folders in the interactive selector (default)
nodeclean

# Preview what would be deleted (no TUI)
nodeclean -dry

# Clean a specific project folder
nodeclean ./my-app

# Recursively scan a directory
nodeclean -r ~/projects

# Delete everything found without prompting (scripts/CI)
nodeclean -y -r ~/projects
```

## Interactive selector (TUI)

By default, after scanning, nodeclean opens a terminal UI so you can choose
exactly which `node_modules` folders to remove:

```
Select node_modules to delete  —  2/5 selected, 1.4 GB

  ▌ [x] /Users/me/projA/node_modules  (820.0 MB)
    [ ] /Users/me/projB/node_modules  (12.0 MB)
    [x] /Users/me/work/api/node_modules  (612.0 MB)

 ↑/↓ move • space toggle • a all/none • s sort (size) • enter delete • q quit
```

- **↑/↓** (or `j`/`k`) — move the cursor
- **space** — toggle the folder under the cursor
- **a** — select all / none
- **s** — sort by size or by path
- **enter** — delete the selected folders (live progress)
- **q** / **esc** — quit without deleting

Nothing is selected by default, so you never delete anything by accident. The
TUI is skipped automatically with `-dry`, `-y`, or when output is piped.

## Flags

| Flag   | Description                                      |
|--------|--------------------------------------------------|
| `-all` | Scan entire disk (default when no path is given)  |
| `-r`   | Recursively scan subfolders for node_modules      |
| `-dry` | Print what would be deleted without deleting       |
| `-y`   | Delete everything found without prompting (no TUI) |

## What gets deleted

- **`node_modules`** — npm/yarn/pnpm dependency folders in your projects

## What gets skipped

nodeclean automatically skips `node_modules` inside tool and IDE directories to avoid breaking your development environment:

- `.vscode`, `.cursor`, `.antigravity` — IDE extensions
- `.copilot` — GitHub Copilot
- `.local` — Neovim Mason packages, etc.
- `.nvm`, `.npm`, `.bun`, `.pnpm`, `.yarn` — version/package managers
- `.cache`, `.config` — tool caches
- `opt` — Homebrew
- `private` — system temp dirs

## How it works

1. Walks the filesystem using **concurrent goroutines** for fast scanning
2. Skips irrelevant directories (`.git`, `Library`, `System`, etc.) and protected tool directories
3. Calculates folder sizes in **parallel** for instant results
4. Shows a live progress indicator during scanning
5. Opens an interactive selector so you choose what to delete
6. Reports total size freed after cleanup

## Uninstall

```bash
rm $(which nodeclean)
```

## License

MIT
