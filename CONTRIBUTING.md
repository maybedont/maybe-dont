# Contributing

## Requirements

- `go` (see `go.mod` for the minimum version)
- `golangci-lint` — linting and formatting
- `goreleaser` — for `make snapshot`
- `cz` ([commitizen](https://commitizen-tools.github.io/commitizen/)) — for
  `make bump-version`
- `upx` (optional) — `brew install upx`; lets `make snapshot` exercise the
  same binary compression used in CI

## Building and testing

```bash
make build   # build the binary
make test    # run the full test suite
make fmt     # auto-format with gofumpt (via golangci-lint)
make lint    # run golangci-lint
make clean   # remove build artifacts
```

Run a single package or test:

```bash
go test -v ./internal/gateway/...
go test -run TestName -v ./...
```

Run `go mod tidy` after any dependency change.

## Git hooks

Install the repo's git hooks once:

```bash
make setup
```

This points `core.hooksPath` at `.githooks/`:

- **pre-commit** auto-formats staged `*.go` files with `golangci-lint fmt`
  and re-stages them.
- **commit-msg** enforces [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
  on commits to `main` (`feat:`, `fix:`, `docs:`, `chore:`, etc.), matching
  the check in `.github/workflows/pr-title.yaml`.

## Testing policies

Policy behavior (CEL and AI rules) is validated by a dedicated test harness,
independent of `go test`:

```bash
maybe-dont test policies --suite-dir ./suite
maybe-dont test policies --suite-dir ./suite --engine cel
maybe-dont test policies --suite-dir ./suite --matrix   # full model matrix
```

See [docs/specs/policy-test-suite.md](docs/specs/policy-test-suite.md) for
the suite format and `internal/config/defaults/tests/` for the example
suite CI runs.

## Docker

```bash
./developer/scripts/buildLocalDockerImage.sh   # builds local/maybe-dont:dev
docker run local/maybe-dont:dev
```

or `make docker-build` / `make docker-run`.

## Releasing

Releases are cut by tag from `main` via
[`.github/workflows/releaser.yaml`](.github/workflows/releaser.yaml).

1. Before bumping, create `release-notes/v{version}.md` in its own PR (copy
   `release-notes/TEMPLATE.md`). The release fails without this file — see
   [docs/specs/release-notes-process.md](docs/specs/release-notes-process.md)
   for the full checklist and content guidelines.
2. After the release-notes PR merges:

   ```bash
   make bump-version
   ```

   This dry-runs `cz bump` to determine the next version, verifies the
   release-notes file exists, then tags and pushes.
3. `make snapshot` builds a local, untagged release with goreleaser for
   testing the release pipeline without publishing anything.

### Troubleshooting `make bump-version`

If it fails, ensure `gpg` is installed and configured for signing, and that
`GPG_TTY` is set:

```bash
export GPG_TTY=$(tty)
```

## Pull requests

- Commit titles and PR titles must follow Conventional Commits (enforced by
  CI and, on `main`, by the `commit-msg` hook).
- Add tests for new or changed behavior; prefer table-driven tests.
- Run `make fmt` and `make lint` before opening a PR.
