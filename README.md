# GitHub stats in Go

<!--
https://github.community/t/support-theme-context-for-images-in-light-vs-dark-mode/147981/84
-->
[![GitHub overview dark](https://github.com/agoodkind/stats/blob/master/generated/overview.svg#gh-dark-mode-only)](https://github.com/agoodkind/stats)
[![GitHub languages dark](https://github.com/agoodkind/stats/blob/master/generated/languages.svg#gh-dark-mode-only)](https://github.com/agoodkind/stats)
[![GitHub overview light](https://github.com/agoodkind/stats/blob/master/generated/overview.svg#gh-light-mode-only)](https://github.com/agoodkind/stats)
[![GitHub languages light](https://github.com/agoodkind/stats/blob/master/generated/languages.svg#gh-light-mode-only)](https://github.com/agoodkind/stats)

`stats-gh` is a Go CLI that collects GitHub profile statistics and writes SVG assets for profile READMEs. It reads configuration from `config.toml`, supports GitHub token and actor values from the environment, and applies recency weighting to owned repository language totals.

## Configuration

Copy `config.toml.example` to `config.toml`, then set the GitHub actor and token inputs. The CLI reads `github.actor` and `github.token` from the config file, and environment variables can provide either value.

- `GITHUB_TOKEN` or `GH_TOKEN` can provide `github.token`.
- `GITHUB_ACTOR` or `GH_ACTOR` can provide `github.actor`.
- `filters.excluded_repos` removes repositories by full name.
- `filters.excluded_langs` removes languages by name.
- `filters.exclude_forked_repos` removes forked repositories from repository-based totals.
- `filters.include_external` includes repositories that the actor contributes to but does not own.
- `recency.half_life` controls how quickly older repository activity decays.
- `recency.floor` keeps older repositories from decaying below a minimum weight.

```toml
[github]
# token may be omitted when GITHUB_TOKEN or GH_TOKEN is set.
# actor may be omitted when GITHUB_ACTOR or GH_ACTOR is set.
actor = "agoodkind"

[filters]
excluded_repos = []
excluded_langs = []
exclude_forked_repos = true
include_external = false

[recency]
half_life = "3y"
floor = 0.05

[logging]
level = "INFO"
```

## Commands

Generate all SVGs:

```bash
go run ./cmd/stats-gh -config ./config.toml generate
```

Print repository inclusion diagnostics:

```bash
go run ./cmd/stats-gh -config ./config.toml diagnose
```

Print the build version:

```bash
go run ./cmd/stats-gh -config ./config.toml version
```

Run the default Go checks:

```bash
make check
```

Generate the SVG assets through `make`:

```bash
make generate
```

## Outputs

- `generated/overview.svg` summarizes stars, forks, contributions, changed lines, views, and repository count.
- `generated/languages.svg` summarizes owned repository language usage with recency weighting.
- `generated/top_repos.svg` ranks repositories by contributor stats activity.
- `diagnose` prints one line per repository with the inclusion reason and recency weight.

## Attribution

This project derives from [`jstrieb/github-stats`](https://github.com/jstrieb/github-stats) by Jacob Strieb and keeps the project under the GNU General Public License v3.0.
