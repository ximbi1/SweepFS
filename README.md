# SweepFS

SweepFS is a terminal storage manager for safe cleanup, movement, copying, and
manual backups. It is inspired by ncdu, but adds a focused UX, previews, and
explicit confirmations for destructive actions.

## Use Cases

- Find the largest folders quickly and clean them safely.
- Move or copy directories after a size review.
- Create manual backups with timestamped folders.

## Install

```bash
go mod tidy
go build -o sweepfs ./cmd/sweepfs
sudo cp sweepfs /usr/local/bin/
```

Then run from anywhere:

```bash
sweepfs
```

Or specify a path:

```bash
sweepfs --path .
```

## Run

```bash
sweepfs
```

## Controls (Quick)

- Navigation: `↑/↓`, `enter` expand/collapse, `→` enter, `←` up
- Selection: `space` toggle select
- Scan: `s`
- Refresh: `r`
- Sort: `o`
- Hidden: `h`
- Search: `/`
- Filters: `e` extension, `z` min size, `x` clear
- Destination: navigate + `p` paste, or type path + `tab` autocomplete
- Backup flow: choose destination → name → compress (y/n)
- Operations: `d` delete, `m` move, `c` copy, `b` backup
- Help: `?`
- Quit: `q`

## Safety

- Destructive actions require explicit confirmation (`y`).
- Recursive delete requires double confirmation.
- Safe mode blocks critical paths: `/`, `$HOME`, `/etc`, `/usr`, `/var`.
- No trash/undo in this MVP.

## Configuration

SweepFS reads `~/.config/sweepfs/config.json` (created on exit). CLI flags
override config values.

Example: `config.example.json`

```json
{
  "path": ".",
  "showHidden": false,
  "safeMode": true,
  "sortMode": "size",
  "theme": "dark",
  "lastDestination": "",
  "keyBindings": {}
}
```

## Build & Distribution

```bash
GOOS=linux GOARCH=amd64 go build -o sweepfs-linux ./cmd/sweepfs
GOOS=darwin GOARCH=arm64 go build -o sweepfs-macos ./cmd/sweepfs
GOOS=windows GOARCH=amd64 go build -o sweepfs.exe ./cmd/sweepfs
```

Or using Make:

```bash
make release
```

Format code:

```bash
make fmt
```

## Limitations

- No cloud sync or compression.
- No trash/undo system.
- No background daemon; scans are manual.

## Release

Current release: `v0.2.0` (see `CHANGELOG.md`).
