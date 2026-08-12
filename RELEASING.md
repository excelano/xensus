# Releasing xensus

The release loop lives in `~/notes/releasing.md` — the ordered steps, the
spent-tag rule, and the standing facts about tokens and secrets. Failure recipes
are in `~/notes/build_release_gotchas.md`. This file carries what is true of
xensus and not of its siblings, and the main thing is that **the loop stops
earlier here than anywhere else in the fleet.**

| | |
|---|---|
| Loop | goreleaser |
| Ships to | the GitHub release, and nowhere else |
| apt | no — deliberately |
| winget | no — deliberately |
| Homebrew | no — `.goreleaser.yml` declares no tap |

**Steps 1 and 2 are the whole procedure.** Run `go build ./... && go test ./...`,
confirm `git status` is clean, then `git tag v1.2.3 && git push origin main
--tags`. goreleaser stamps the binary from the tag, so there is no version to
bump and no file in the repo carries the number. One job produces the platform
archives for Linux, macOS, and Windows, the amd64 and arm64 `.deb` packages,
`checksums.txt`, `install.sh`, `uninstall.sh`, and the GitHub Release. Then stop.

**Do not run `apt-ship`.** xensus is a self-hosted server rather than a
workstation CLI, so it is installed by download on the box that will run it. The
`.deb` packages are release assets for `dpkg -i`, not inputs to the apt repo, and
`fleet -r` reports its `APT` column as `-` rather than `behind` because it is not
an apt-shipped tool. A release with no apt step is a finished release here, which
is the opposite of the rule everywhere else in the fleet.

**There is no winget submission and no Homebrew formula,** so the winget half of
the shared procedure — the fork sync, komac, the validation failures — does not
apply. What still applies is the spent-tag rule: `install.sh` resolves the latest
tag through the GitHub API, so re-cutting a tag swaps the asset out from under
anyone who pinned that version with `XENSUS_VERSION`.

**The release workflow has no `workflow_dispatch` fallback,** so it only ever
runs from a real tag push. If a tag lands without triggering it, the fix is to
delete and re-push the tag by hand, or add the dispatch input.

**The tests cannot see a tenant.** xensus signs users in over M365 OIDC and
writes to SQLite, and a clean test run says the code compiles and the pure logic
holds. Sign in against a real tenant on the new build before tagging, and give
anything that mints or re-associates a person ID the extra pass, since an ID that
gets reused is the one failure the design exists to prevent.
