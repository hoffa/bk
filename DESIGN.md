# Design

## Status colors

A backup's currency is a color:

| Color  | Meaning                                                       |
| ------ | ------------------------------------------------------------ |
| green  | synced — source refs match what was last backed up           |
| yellow | stale — the source has commits not yet backed up             |
| grey   | never synced                                                 |
| red    | error (id mismatch, not a backup, unreadable source)         |
| cyan   | syncing right now (dashboard only, transient)                |

When the target is **present**, green/yellow are verified against it. When the
target is **absent** (e.g. an unplugged drive) the verdict is inferred from the
cached refs hash and shown **dimmed** — a muted green still means "your work is
safe on a drive that's just unplugged". Errors are always red.

### Live dashboard (`bk` in a terminal)

Each backup is a fat status dot (`●`) in the color above; an absent target dims
the dot. So a bright green dot is "synced and plugged in", a dim green dot is
"safe but unplugged", grey is never synced, red is broken.

### Plain output (`bk status`, piped, or CI)

A colorless text table carries more nuance than the dot: short ASCII codes, with
a trailing `?` meaning *unverified* (target absent, verdict inferred from the
cached refs):

| Code     | Meaning                                                       |
| -------- | ------------------------------------------------------------ |
| `OK`     | synced, verified against the target                          |
| `OK?`    | believed synced, but offline so unverified                   |
| `STALE`  | confirmed out of date — target present, repo has new commits |
| `STALE?` | believed out of date (from the last sync), but offline       |
| `NEW`    | never synced                                                  |
| `ERROR`  | error                                                         |
