# Changelog

## v0.1.0
- Initial public release.
- Fast TUI navigation with dual-panel layout and safe defaults.
- Real filesystem scanning with size aggregation and progress feedback.
- Safe actions: delete, move, copy, backup (preview + confirmation).
- Backup flow with destination selection, name prompt, optional compression.
- Config persistence with theme, sort, hidden toggle, last destination.
- Cross-platform build support and Makefile targets.

## v0.2.0
- Quick search (`/`) and advanced filters (extension, min size, clear).
- Destination paste (`p`) and path autocomplete with suggestions.
- Backup name prompt with optional compression.
- Incremental scan cache for faster rescans.

## v0.2.1
- Fix delete to remove directories reliably and refresh listing.
- Improve delete error messaging with first failure reason.
- Open at current working directory when run with sudo.
