# nodeclean

A fast CLI tool to find and remove `node_modules` folders from your disk, freeing up gigabytes of space.

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
# Scan entire disk (default — no arguments needed)
nodeclean

# Preview what would be deleted
nodeclean -dry

# Clean a specific project folder
nodeclean ./my-app

# Recursively scan a directory
nodeclean -r ~/projects

# Combine flags
nodeclean -dry -r ~/projects
```

## Flags

| Flag   | Description                                      |
|--------|--------------------------------------------------|
| `-all` | Scan entire disk (default when no path is given)  |
| `-r`   | Recursively scan subfolders for node_modules      |
| `-dry` | Print what would be deleted without deleting       |

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
5. Asks for confirmation before deleting
6. Reports total size freed after cleanup

## Uninstall

```bash
rm $(which nodeclean)
```

## License

MIT
