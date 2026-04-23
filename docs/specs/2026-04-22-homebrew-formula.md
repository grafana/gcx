---
type: refactor-spec
title: "Ship gcx via a Homebrew formula (build-from-source) instead of a cask"
status: draft
created: 2026-04-22
---

# Ship gcx via a Homebrew formula (build-from-source) instead of a cask

## Current Structure

PR #537 adds Homebrew distribution for gcx via a **cask** that downloads the
pre-built release binary:

```
goreleaser ──► .goreleaser.yaml (homebrew_casks: stanza)
            │   ├─► renders dist/homebrew/Casks/gcx.rb (binary-install cask)
            │   └─► attaches gcx.rb to GitHub release (release.extra_files)
            │
            └─► release.yaml (binaries + checksums + release)
                   │
                   └─► publish-homebrew-cask.yml (needs: release)
                          ├─► gh release download gcx.rb asset
                          ├─► Vault: fetch App creds
                          ├─► mint App installation token
                          ├─► clone grafana/homebrew-grafana
                          ├─► cp gcx.rb → Casks/gcx.rb
                          ├─► idempotency: git diff --cached --quiet → exit 0
                          └─► commit + force-push + gh pr create
```

**User install command**: `brew install --cask grafana/grafana/gcx`

**Files added/modified by PR #537:**
- `.goreleaser.yaml` (add `homebrew_casks:` stanza, `release.extra_files` glob)
- `.github/workflows/publish-homebrew-cask.yml` (new, 199 lines)
- `.github/workflows/release.yaml` (drop dead `HOMEBREW_TAP_GITHUB_TOKEN`)
- `.gitignore` (ignore `dist/homebrew/`)
- `README.md`, `docs/installation.md`, `CONTRIBUTING.md` (install instructions + Gatekeeper workaround section)

### Pain points motivating this refactor

1. **macOS blocks the unsigned binary.** The gcx release binary is not
   Apple-notarised and has no Developer ID signature. On Apple Silicon the
   kernel's signing check kills the process on first run (`killed: 9`); on
   Intel, Gatekeeper shows an "Apple could not verify…" dialog. Homebrew
   5.0 removed the `--no-quarantine` flag, so `brew install --cask` now
   leaves `com.apple.quarantine` on the binary and these checks fire.
2. **The docs workaround is user-hostile.** PR #537 ships a documented
   `xattr -d com.apple.quarantine && codesign --sign -` recipe. Every
   first-time installer hits this, and "run two opaque shell commands to
   make the tool work at all" is a bad first impression for a CLI.
3. **It diverges from the Grafana Labs norm.** `alloy`, `grafana`,
   `mimirtool`, `cortextool`, and `grafana-agent` all ship as classical
   build-from-source formulas in `grafana/homebrew-grafana`. gcx would be
   the only Grafana CLI using a binary cask — both operationally (the
   custom cask workflow) and from the user's perspective (`--cask` flag,
   Gatekeeper workaround).
4. **Apple-notarisation is tracked as a separate ADR and may be weeks
   out.** Shipping a formula unblocks ergonomic Homebrew distribution
   today without waiting for signing infra.

## Target Structure

Switch to a build-from-source **formula**, matching the pattern used by
`alloy.rb` and `grafana.rb` in `grafana/homebrew-grafana`:

```
goreleaser ──► .goreleaser.yaml (Homebrew stanzas removed)
            │
            └─► release.yaml (binaries + checksums + release — unchanged)
                   │
                   └─► publish-homebrew-formula.yml (needs: release)
                          ├─► pre-release guard (skip -rc/-dev tags)
                          ├─► checkout gcx @ tag (for template)
                          ├─► curl source tarball + compute sha256
                          ├─► envsubst gcx.rb.tmpl → gcx.rb
                          ├─► Vault: fetch App creds
                          ├─► mint App installation token
                          ├─► clone grafana/homebrew-grafana
                          ├─► cp gcx.rb → gcx.rb  (tap root, not Casks/)
                          ├─► idempotency: git diff --cached --quiet → exit 0
                          └─► commit + force-push + gh pr create
```

**User install command**: `brew install grafana/grafana/gcx` (no `--cask`).

**Key structural changes:**

- GoReleaser no longer emits anything Homebrew-related. The `brews:` stanza
  only supports binary-install formulas; since we're moving to
  build-from-source, we hand-roll the formula from a template entirely
  inside the publish workflow.
- Formula lives at **the root** of `grafana/homebrew-grafana` (`gcx.rb`),
  flat-layout alongside `alloy.rb` and `grafana.rb`, not under `Casks/`.
- The formula template is checked into this repo at
  `.github/homebrew/gcx.rb.tmpl` with `${VERSION}` and `${SHA256}`
  placeholders rendered by `envsubst` at publish time.
- The Vault / GitHub App plumbing for tap-PR creation is preserved as-is.

### Formula template

The template below is checked in at `.github/homebrew/gcx.rb.tmpl`. The
`${VERSION}` and `${SHA256}` placeholders are substituted by `envsubst`
at publish time; Ruby's `#{...}` interpolations (`#{version}`,
`#{time.iso8601}`) are left alone (envsubst is invoked with an
allow-list of only `$VERSION $SHA256`).

```ruby
class Gcx < Formula
  desc "Grafana Cloud CLI"
  homepage "https://github.com/grafana/gcx"
  url "https://github.com/grafana/gcx/archive/refs/tags/v${VERSION}.tar.gz"
  sha256 "${SHA256}"
  license "Apache-2.0"
  head "https://github.com/grafana/gcx.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X main.version=v#{version}
      -X main.commit=homebrew
      -X main.date=#{time.iso8601}
    ]

    system "go", "build",
      "-buildvcs=false",
      *std_go_args(ldflags: ldflags, output: bin/"gcx"),
      "./cmd/gcx"

    generate_completions_from_executable(bin/"gcx", "completion")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/gcx --version")
  end
end
```

**Why these choices:**

- **`depends_on "go" => :build`** (floating, not `go@1.26`) — matches
  Alloy and `gh`. Homebrew-core's `go` is currently `1.26.2`, compatible
  with gcx's `go 1.26.0` floor. `go@1.26` doesn't exist yet as a
  versioned formula. Accepted risk: transient breakage if our `go.mod`
  pin outpaces homebrew-core's `go`.
- **`-buildvcs=false`** — source tarballs from GitHub's
  `archive/refs/tags/` don't contain `.git`, so `go build` would
  otherwise fail stamping VCS info.
- **`main.commit=homebrew`** — sentinel value. We don't have the real
  commit SHA inside the install block, and threading it through the
  workflow adds churn for low value. A `commit=homebrew` stamp is a
  clear tell that the binary came from a Homebrew build.
- **`std_go_args`** — Homebrew helper, adds `-trimpath` and standardises
  flag ordering.
- **`head` stanza** — enables `brew install --HEAD grafana/grafana/gcx`
  for users who want to run tip-of-main.
- **`generate_completions_from_executable`** — auto-installs
  bash/zsh/fish completions via `gcx completion <shell>`. Replaces the
  cask's `completion` caveat.
- **No `caveats`, `zap`, `conflicts`** — the cask-era stanzas don't
  apply: completions are now auto-installed, `zap` is a cask-only
  concept, and there's no naming collision with other
  formulas/casks.

### Publish workflow sketch

```yaml
# .github/workflows/publish-homebrew-formula.yml  (key steps only)

steps:
  - uses: actions/checkout@v4
    with: { ref: ${{ inputs.tag }} }

  - name: Resolve tag and pre-release guard
    # unchanged: skip tags with '-' (rc/dev/etc.)

  - name: Compute source tarball sha256
    run: |
      TARBALL_URL="https://github.com/${GH_REPO}/archive/refs/tags/${TAG}.tar.gz"
      curl -fsSL --retry 3 --retry-delay 5 -o source.tar.gz "${TARBALL_URL}"
      SHA256="$(sha256sum source.tar.gz | awk '{print $1}')"
      [[ "${SHA256}" =~ ^[0-9a-f]{64}$ ]] || { echo "::error::bad sha256"; exit 1; }
      echo "sha256=${SHA256}" >> "${GITHUB_OUTPUT}"

  - name: Render formula from template
    env:
      VERSION: ${{ steps.tag.outputs.version }}
      SHA256: ${{ steps.sha.outputs.sha256 }}
    run: |
      envsubst '$VERSION $SHA256' \
        < .github/homebrew/gcx.rb.tmpl > gcx.rb
      grep -q "^class Gcx < Formula" gcx.rb \
        || { echo "::error::bad formula render"; exit 1; }
      grep -q "sha256 \"${SHA256}\""    gcx.rb \
        || { echo "::error::sha256 not substituted"; exit 1; }

  # Vault creds, App token, clone tap, commit, push, PR — unchanged
  # from the cask workflow except the file path is gcx.rb at the root
  # instead of Casks/gcx.rb.
```

## Behavioral Contract

### Invariants

- Release pipeline for binaries (`goreleaser` + `release.yaml`) is
  **unchanged**. Binary releases, checksums, release notes, and asset
  uploads behave identically to PR #537.
- The `curl | sh` install script path is **unchanged** — still downloads
  the release tarball, clears quarantine, and installs to
  `~/.local/bin`.
- Manual download of release binaries from the GitHub Releases page is
  **unchanged** — the Gatekeeper workaround section in
  `docs/installation.md` still applies to this path.
- The Vault / GitHub App credential model is **unchanged** — same App,
  same Vault paths, same scope to `grafana/homebrew-grafana`.
- Pre-release tags (`v*-rc.*`, `v*-dev.*`) are **not** published to the
  tap (same guard as PR #537's cask workflow).
- Re-running the publish workflow on the same tag is **idempotent** (no
  duplicate PRs, clean exit on byte-identical renders).

### Intentional Changes

1. **User install command changes** from `brew install --cask
   grafana/grafana/gcx` to `brew install grafana/grafana/gcx`.
   Justification: build-from-source formulas don't use `--cask`. The
   cask never shipped to users (PR #537 is still open), so there's no
   migration burden.
2. **First-install time increases** from seconds (binary download) to
   ~15–60 seconds (Go build + module fetch from
   `proxy.golang.org`). Justification: trade a one-time install cost
   for eliminating the Gatekeeper workaround entirely. Matches Alloy
   and every other Grafana CLI formula.
3. **Homebrew path no longer requires `xattr -d` / `codesign -`**. The
   Gatekeeper workaround section in `docs/installation.md` is
   **retained** for the `curl | sh` and manual-download paths but its
   Homebrew mention is removed.
4. **No caveats on install.** The cask showed auth/completion/Gatekeeper
   caveats. The formula shows none — completions are auto-installed,
   Gatekeeper doesn't apply, and auth guidance belongs in the README /
   docs (same pattern as Alloy, which only caveats for actual
   config-file management).

## Migration Steps

Each step leaves the repo in a build-able state. Steps 1–2 add the new
artifacts alongside the old ones; step 3 flips the release-workflow
trigger; steps 4–6 remove the cask-era code and update docs. Verification
is only possible end-to-end after step 3, but per-step local checks catch
most issues earlier.

1. **Add formula template** at `.github/homebrew/gcx.rb.tmpl` (contents
   per "Formula content" above).
   **Verify**: manual `VERSION=0.2.8 SHA256=... envsubst ... < tmpl` renders
   a formula that passes `brew style` and `brew audit --formula`.

2. **Add `publish-homebrew-formula.yml` workflow** alongside the existing
   cask workflow. Mark it `workflow_dispatch`-only initially for safe
   manual testing.
   **Verify**: manually dispatch against a past stable tag; the workflow
   opens a PR against `grafana/homebrew-grafana` at `gcx.rb` (tap root)
   with correct version and sha256. Do **not** merge this test PR — close
   it.

3. **Flip the release trigger** in `.github/workflows/release.yaml` from
   `publish-homebrew-cask.yml` to `publish-homebrew-formula.yml`. Add
   `workflow_call` to the new workflow.
   **Verify**: local `act` / workflow syntax check; no release cut needed
   yet.

4. **Remove `homebrew_casks:` stanza and `release.extra_files` glob**
   from `.goreleaser.yaml`.
   **Verify**: `goreleaser check` (or `goreleaser release --snapshot
   --skip=publish --clean`) succeeds. No `dist/homebrew/` output is
   produced.

5. **Delete `.github/workflows/publish-homebrew-cask.yml`** and drop the
   `dist/homebrew/` entry from `.gitignore`.
   **Verify**: `git grep -i "publish-homebrew-cask"` returns nothing.

6. **Update docs**:
   - `README.md`: replace `--cask` with plain `brew install`; remove the
     Gatekeeper cross-link from the Homebrew entry.
   - `docs/installation.md`: same install-command swap; remove the
     Homebrew-specific parenthetical from the Gatekeeper section; the
     section itself stays useful for `curl | sh` and manual-download
     paths.
   - `CONTRIBUTING.md`: update the PR #537 note to point at the
     formula workflow + template file, call out that the formula is
     **not** generated by GoReleaser (to avoid surprising future
     maintainers who look at `.goreleaser.yaml` first).
   **Verify**: `make docs` (with `GCX_AGENT_MODE=false`) builds cleanly.

7. **End-to-end release smoke test** (done once on the next stable
   release cut, e.g., `v0.2.9`):
   - Observe `publish-homebrew-formula` run after release completes.
   - Review the generated tap PR (URL, sha256, version all correct).
   - Merge tap PR.
   - On a clean macOS (Apple Silicon): `brew install
     grafana/grafana/gcx` → compiles, `gcx --version` prints `v0.2.9`,
     no Gatekeeper dialog, no `killed: 9`.

## Acceptance Criteria

- GIVEN a stable release tag `vX.Y.Z` is cut
  WHEN the release workflow completes
  THEN `publish-homebrew-formula.yml` runs and opens a PR against
       `grafana/homebrew-grafana` modifying `gcx.rb` at the tap root
       with `version = X.Y.Z` and `sha256` matching the GitHub source
       tarball.

- GIVEN a pre-release tag `vX.Y.Z-rc.N` is cut
  WHEN the release workflow completes
  THEN `publish-homebrew-formula.yml` exits 0 without opening a tap PR.

- GIVEN `publish-homebrew-formula.yml` has already opened a PR for tag
       `vX.Y.Z`
  WHEN the workflow is manually re-dispatched on the same tag
  THEN no duplicate PR is created; if the re-render is byte-identical
       to the default branch's `gcx.rb` the job exits 0 without
       pushing, otherwise the existing branch is force-pushed with the
       updated render and the existing PR is left in place.

- GIVEN a user on Apple Silicon macOS with the tap merged and no gcx
       previously installed
  WHEN they run `brew install grafana/grafana/gcx`
  THEN the formula compiles from source with no Gatekeeper dialog, no
       `killed: 9`, and `gcx --version` prints `vX.Y.Z`.

- GIVEN a user who installed gcx via Homebrew
  WHEN they run `which gcx` and `ls $(brew --prefix)/share/zsh/site-functions/_gcx`
  THEN the binary resolves under `$(brew --prefix)/bin/gcx` and the zsh
       completion file exists.

- GIVEN a user runs `brew test gcx`
  WHEN the test block executes
  THEN it passes (the version string round-trips via `gcx --version`).

- GIVEN a developer reads `.goreleaser.yaml` after this change
  WHEN they look for Homebrew-related configuration
  THEN they find none, and `CONTRIBUTING.md` directs them to
       `.github/workflows/publish-homebrew-formula.yml` and
       `.github/homebrew/gcx.rb.tmpl`.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Homebrew-core's `go` formula drifts below gcx's `go.mod` floor (currently 1.26.0) | `brew install` fails with a Go compile error | Accepted; matches Alloy's model. If it happens, either bump to `depends_on "go@1.26" => :build` when that versioned formula lands, or temporarily lower our `go.mod` floor. |
| GitHub changes its source-tarball generation and sha256 shifts for an already-published tag | Existing tap formula sha256 no longer matches; `brew install` fails with a checksum mismatch | Low probability (GH's archive format has been stable for years). Mitigation: manually re-dispatch the workflow to rebuild the tap PR with the new sha256. |
| First-install performance regression (~15–60s Go compile vs. binary download) confuses users | Perception that `brew install gcx` is slow | Documented in `docs/installation.md` alongside the install command. Consistent with Alloy and every other Grafana formula — users are accustomed. |
| Transient module-proxy outage during `brew install` | User sees an opaque Go module fetch error | Out of scope — same failure mode as Alloy and `gh`. Users can retry; Go module proxy uptime is high. |
| `envsubst` not available on the runner | Render step fails | `envsubst` ships in the `gettext` package and is present by default on `ubuntu-latest` GitHub runners. Pinning a concrete runner version would over-engineer this. |
| A future gcx version needs non-Go build tools (e.g., `node` for embedded UI) | Formula build fails | Add the relevant `depends_on` to the template. Same migration path Alloy uses (they already depend on `node@24` for their embedded UI). |
| `gcx --version` output format drifts and breaks the formula `test` block assertion | `brew test gcx` fails in tap CI | Caught before merge by tap CI; trivial to fix by tightening or relaxing the `assert_match` pattern. |

## Out of Scope

- Apple notarisation and Developer ID signing of release binaries
  (tracked in a separate ADR; this refactor makes it *less* urgent by
  moving the primary macOS install path off unsigned-binary territory).
- Publishing gcx to homebrew-core (the public default tap). That would
  require meeting homebrew-core's stricter policies (vendoring, no
  resource downloads during install, etc.) and an upstream review
  process. Not a goal of this change.
- Changes to the `curl | sh` install script or manual-download
  instructions beyond removing the Homebrew-specific language from the
  Gatekeeper section.
