# Design

## Status colors

The dashboard shows each backup as a colored `⏺`. Color encodes currency,
brightness encodes connection:

| Color        | Meaning                                                            |
| ------------ | ----------------------------------------------------------------- |
| green        | online and latest — synced to the latest commit, and verified     |
| muted green  | offline and latest — synced to the latest commit, but can't verify |
| yellow       | online and stale — target reachable, repo has changed since sync  |
| muted yellow | offline and stale — repo has changed since the last known sync    |
| muted        | never synced                                                      |
| red          | error (id mismatch, not a backup, unreadable source)              |

"Latest" means the source repo's refs match what was last backed up. When the
target is present this is verified against the target itself; when offline it is
inferred from the last sync recorded in the config.
