# Design

## Status

A backup's currency is a state:

| State  | Meaning                                                       |
| ------ | ------------------------------------------------------------ |
| synced | source refs match what was last backed up                    |
| stale  | the source has commits not yet backed up                     |
| new    | never synced                                                  |
| error  | id mismatch, not a backup, unreadable source, or similar     |

When the target is **present**, synced/stale are verified against it. When the
target is **absent** (e.g. an unplugged drive), the verdict is inferred from the
cached refs hash and marked with an `_OFFLINE` suffix. Errors are reported as
`ERROR`.

### Plain output (`bk status`, piped, or CI)

Headerless, tab-separated, one record per line — every column machine-readable so
`cut`/`awk`/`read` consume it without skipping. Columns, in order: id, state,
source, target, last sync (RFC 3339, empty if never) -- id and state first, to
match `bk sync`. The state carries online/offline nuance:
an `_ONLINE` / `_OFFLINE` suffix records whether the verdict was confirmed
against a present target or inferred from the cache while it was absent.

| Code             | Meaning                                                       |
| ---------------- | ------------------------------------------------------------ |
| `SYNCED_ONLINE`  | synced, verified against the target                          |
| `SYNCED_OFFLINE` | believed synced, but offline so unverified                   |
| `STALE_ONLINE`   | confirmed out of date — target present, repo has new commits |
| `STALE_OFFLINE`  | believed out of date (from the last sync), but offline       |
| `NEW`            | never synced                                                  |
| `ERROR`          | error                                                         |
