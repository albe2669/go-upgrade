# go-upgrade

Interactive TUI for updating Go module dependencies. Lists all direct dependencies with available updates, lets you select which to upgrade, and runs the updates with a progress bar.

## Installation

### Go install

```bash
go install go-upgrade@latest
```

### Binary releases

Download pre-built binaries from the [Releases](https://github.com/arn/go-upgrade/releases) page.

## Usage

Run `go-upgrade` in any directory containing a `go.mod` file:

```bash
go-upgrade
```

The tool will:

1. Scan for outdated direct dependencies
2. Present an interactive selection list
3. Update selected dependencies
4. Run `go mod tidy`

### Keybindings

| Key | Action |
|-----|--------|
| `j` / `Down` | Move cursor down |
| `k` / `Up` | Move cursor up |
| `Space` | Toggle selection |
| `a` | Select / deselect all |
| `Enter` | Confirm and start updating |
| `q` / `Ctrl+C` | Quit |

## Building from source

```bash
git clone https://github.com/arn/go-upgrade.git
cd go-upgrade
go build -o go-upgrade .
```
