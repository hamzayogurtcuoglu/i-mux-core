# Changelog

All notable changes to i-mux-core are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- The repository skeleton: `imux-core -V`, the package layout, and `make ci`.
- CI on Linux, macOS and Windows, and the pinned tmux 3.8 (`make tmux`) the differential tests
  compare against.
- The contract recorded from i-mux (`make contract-record`, `make contract`): COMPATIBILITY.md now
  lists exactly what i-mux uses of tmux 3.8, with `contract/expected.json` as its data.
- The differential test harness (`make difftest`): scripts in `testdata/diff` run against tmux
  and i-mux-core, and every command's output, exit code and captured screen is compared.
- The terminal emulator's parser, ported from tmux 3.8's `input.c` (E10).
