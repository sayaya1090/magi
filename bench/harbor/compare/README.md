# The head-to-head page

Builds the comparison report from a Terminal-Bench 2.1 run of magi and the published
Claude Code leaderboard row, and freezes enough of both sides that a clone can rebuild
the same page without the original run directories.

```sh
python3 bench/harbor/compare/build_page.py --lang=en docs/bench/tb21-magi-vs-claude-code.html
python3 bench/harbor/compare/build_page.py --lang=ko docs/bench/tb21-magi-vs-claude-code.ko.html
```

The two pages are one measurement and two sets of sentences. Numbers, verdicts and links come from
the same scan; the prose lives in `text_en.py` and `text_ko.py`, whose keys have to match, and the
page chrome in `page.en.html` and `page.ko.html`. A card written on only one side is a card the
other language silently drops, so add to both or to neither.

## Where each number comes from

| | |
|---|---|
| magi's verdicts | `../runs/tb21-2026-08/*.tar.gz` — one archive per adopted trial, or `jobs/` when present |
| magi's cost and duration | `ledger.tsv`, frozen from the run logs (which are not in the repository) |
| Claude Code | `cc89.tsv`, scraped from leaderboard row `d7540f21` and adjusted for its disqualifications; `cc89.raw.tsv` keeps the unadjusted counts |
| Claude Code's web use | `cc_web.json`, from its 439 trajectories |
| held trials | `quarantine.tsv` |

`jobs/` wins over the archives when both are there, so a re-run shows up without touching
anything else. The archives carry only adopted trials, so the quarantine and `ADOPT` rules
have nothing left to act on and both paths land on the same page.

## The rules the page applies

**A trial that went looking for the answer is held**, its task re-run, and the re-run adopted —
`quarantine.tsv` names each one and why. A hold reaches every trial of that task at or before it,
so a table never shows a number the rule did not adopt.

**`ADOPT` pins a task whose answer is a famous public repository.** Every attempt at `regex-chess`
reached the answer, so there is no clean trial to adopt and the row would vanish; naming the run
keeps the row, and the caveat beside it does the work the hold cannot.

**`REWARD_HACK` rescores a trial to zero**, which is what tbench.ai's leaderboard integrity policy
prescribes for a model that resolves a task without demonstrating the capability it measures. Both
sides are scored this way: the Claude Code figure here is the leaderboard's own post-adjustment
74.61%, not the 75.28% its raw rewards sum to.

## Auditing

- `audit_web.py` — every web call in a run, flagged against dataset, solution and grader patterns.
  The dataset answers to two names (`harbor-framework` and `harborframework.com`); both are matched.
- `rc_clean.py` — judges one `regex-chess` trial by what its web calls **returned**, not by which
  URL they touched: a GitHub tree page renders client-side and gives back nothing, while a
  `raw.githubusercontent.com` fetch is the file itself.
- `is_adopted.py` — exit 0 when a task has a trial the tables adopt.
