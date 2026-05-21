# GitHub stats in Go

<!--
https://github.community/t/support-theme-context-for-images-in-light-vs-dark-mode/147981/84
-->
[![GitHub overview dark](https://github.com/agoodkind/stats/blob/master/generated/overview.svg#gh-dark-mode-only)](https://github.com/agoodkind/stats)
[![GitHub languages dark](https://github.com/agoodkind/stats/blob/master/generated/languages.svg#gh-dark-mode-only)](https://github.com/agoodkind/stats)
[![GitHub top repos dark](https://github.com/agoodkind/stats/blob/master/generated/top_repos.svg#gh-dark-mode-only)](https://github.com/agoodkind/stats)
[![GitHub overview light](https://github.com/agoodkind/stats/blob/master/generated/overview.svg#gh-light-mode-only)](https://github.com/agoodkind/stats)
[![GitHub languages light](https://github.com/agoodkind/stats/blob/master/generated/languages.svg#gh-light-mode-only)](https://github.com/agoodkind/stats)
[![GitHub top repos light](https://github.com/agoodkind/stats/blob/master/generated/top_repos.svg#gh-light-mode-only)](https://github.com/agoodkind/stats)

`stats-gh` is a Go CLI that collects GitHub profile statistics and writes three SVG assets (overview, language breakdown, top-repos cards) for your profile README. Every aggregation rule, scoring weight, and display option is driven by `config.toml`.

## Outputs

| File | Contents |
| ---- | -------- |
| `generated/overview.svg` | Header card with totals: Stars, Forks, All-time contributions, Lines of code changed, lifetime Repository views, public repos you own, open-source repos you contribute to. |
| `generated/languages.svg` | Language breakdown across your active owned repos (optionally folded in with externals). Percentages are compressed so a single dominant language doesn't crowd out the rest. |
| `generated/top_repos.svg` | 2-column card grid of your top repos. Each card shows the name, primary-language dot, description, star count, and last-pushed-ago. |
| `generated/views_history.json` | Persisted per-repo daily traffic counts; the bot commits this alongside the SVGs so the Repository-views number accumulates across runs (the API only exposes the rolling 14-day window). |
| `diagnose` subcommand stdout | One line per repository with the inclusion reason and recency weight. Not committed; for debugging. |

## Configuration knobs

Every section is optional; defaults below are applied to anything you omit.

### `[github]`

| Key | Default | Effect |
| --- | --- | --- |
| `token` | env `GH_TOKEN` then `GITHUB_TOKEN` | PAT used for every GitHub API call. `GH_TOKEN` takes priority so a personal token overrides the limited GitHub Actions default. |
| `actor` | env `GH_ACTOR` then `GITHUB_ACTOR` | GitHub login the stats are computed for. |

### `[logging]`

| Key | Default | Effect |
| --- | --- | --- |
| `level` | `"INFO"` | slog level: `DEBUG`, `INFO`, `WARN`, `ERROR`. |

### `[recency]`

Each commit you authored is weighted as `max(floor, 0.5 ^ (commit_age / half_life))`. The weighted sum drives the top-repos score and scales each repo's language-byte contribution.

| Key | Default | Effect |
| --- | --- | --- |
| `half_life` | `"3y"` | How long a commit takes to count for half. Accepts `"<years>y"` or any `time.ParseDuration` string (`"180d"`, `"8760h"`). Larger = older commits count more. |
| `floor` | `0.05` | Per-commit weight floor: a commit can never count for less than this. Set to `1.0` to disable recency entirely; `0` to let old commits go to zero. |

### `[owned]`

Inclusion rules for the public repos you own. A repo that fails any rule gets dropped from the Stars / Forks / "Public repos I own" counts, the language stats, and the top-repos cards.

| Key | Default | Effect |
| --- | --- | --- |
| `exclude_archived` | `true` | Drop archived repos. |
| `exclude_disabled` | `true` | Drop disabled (suspended) repos. |
| `exclude_forks` | `true` | Drop repos you forked from someone else. |
| `require_languages` | `true` | Drop repos GitHub has no language data for (typically empty repos). |
| `excluded_repos` | `[]` | Full `owner/name` strings to drop. |
| `excluded_langs` | `[]` | Case-insensitive language names to drop from the language chart. |

### `[contributed]`

External repos you've committed to but don't own (GitHub's `repositoriesContributedTo` set). External repo *names* never render in any SVG — only aggregate numbers and language bytes can flow through.

| Key | Default | Effect |
| --- | --- | --- |
| `include` | `"all"` | `"all"` includes private (SSO-gated) externals in the "Open-source repos I contribute to" tally. `"public-only"` filters them out. |
| `include_in_loc` | `true` | Sum external commit additions/deletions into "Lines of code changed". Requires the PAT to be SSO-authorized for the org for private externals. |
| `include_in_languages` | `true` | Roll external repos' language bytes into the Languages chart (e.g. a Ruby work repo lifts the displayed Ruby %). |

### `[top_repos]`

The card grid.

| Key | Default | Effect |
| --- | --- | --- |
| `limit` | `6` | Number of cards rendered. 2 columns wide, so an odd number leaves the last row half-empty. |
| `star_coefficient` | `2.0` | `score = log10(1 + weighted_commits) + star_coefficient * log10(1 + stars)`. Higher → popularity outliers dominate; `1.0` balances commits and stars; `0` ignores stars entirely. |

### `[languages]`

| Key | Default | Effect |
| --- | --- | --- |
| `compression` | `"sqrt"` | Curve applied to weighted byte totals before percentages are computed. `"linear"` shows raw ratios (one dominant language stays dominant). `"sqrt"` makes smaller languages visible. `"log"` is the most aggressive flattening. |

### `[views]`

The Repository-views number on the Overview is `seed + every daily count ever fetched`. GitHub's traffic API only exposes the trailing 14 days, so the bot persists daily counts in `generated/views_history.json` each run.

| Key | Default | Effect |
| --- | --- | --- |
| `seed` | `0` | Starting offset, useful for picking up a prior counter (e.g. komarev's badge value) so the displayed number doesn't reset to zero. Edits to this value override the seed in the on-disk history file on the next run. |

### Full example

```toml
[github]
actor = "agoodkind"

[logging]
level = "INFO"

[recency]
half_life = "3y"
floor = 0.05

[owned]
exclude_archived = true
exclude_disabled = true
exclude_forks = true
require_languages = true
excluded_repos = []
excluded_langs = []

[contributed]
include = "all"
include_in_loc = true
include_in_languages = true

[top_repos]
limit = 6
star_coefficient = 2.0

[languages]
compression = "sqrt"

[views]
seed = 1152
```

## Commands

```bash
go run ./cmd/stats-gh -config ./config.toml generate    # write SVGs to generated/
go run ./cmd/stats-gh -config ./config.toml diagnose    # print per-repo inclusion decisions
go run ./cmd/stats-gh -config ./config.toml version     # print build version
make check                                               # lint + format gates
make generate                                            # shorthand for the generate command
```

## Attribution

This project derives from [`jstrieb/github-stats`](https://github.com/jstrieb/github-stats) by Jacob Strieb and keeps the project under the GNU General Public License v3.0.
