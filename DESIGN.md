# Design

## Status badges

The dashboard shows each backup as a colored status badge (ASCII, like a test
runner's PASS/FAIL). The code is the verdict, the color reinforces it, and a
trailing `?` means *unverified* — the target is absent, so the verdict is
inferred from the last sync recorded in the config rather than confirmed against
the target itself.

| Badge    | Color  | Meaning                                                       |
| -------- | ------ | ------------------------------------------------------------ |
| `OK`     | green  | synced to the latest commit, verified against the target     |
| `OK?`    | green  | believed synced to the latest commit, but offline so unverified |
| `STALE`  | yellow | confirmed out of date — target present, repo has new commits |
| `STALE?` | yellow | believed out of date (from the last sync), but offline so unverified |
| `NEW`    | grey   | never synced                                                 |
| `ERR`    | red    | error (id mismatch, not a backup, unreadable source)         |

"Latest" means the source repo's refs match what was last backed up. When the
target is present this is verified against it; when absent it is inferred from
the cached refs, hence the `?`.
