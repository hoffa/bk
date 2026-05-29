# bk

[![check](https://github.com/hoffa/bk/actions/workflows/check.yml/badge.svg)](https://github.com/hoffa/bk/actions/workflows/check.yml)
[![release](https://github.com/hoffa/bk/actions/workflows/release.yml/badge.svg)](https://github.com/hoffa/bk/actions/workflows/release.yml)

Versioned git repository backups using append-only git bundles.

## Install

```sh
brew install hoffa/tap/bk
```

Or with Go:

```sh
go install github.com/hoffa/bk@latest
```

Or download a [prebuilt binary](https://github.com/hoffa/bk/releases/latest).

## Usage

Register a repository and a backup directory. This only edits the config;
the target doesn't need to exist yet (e.g. an unplugged drive):

```sh
bk add ~/code/my-repo /Volumes/usb/my-repo
```

Back up everything you've registered:

```sh
bk sync
```

Or back up one configured pair by the id shown in `bk status`:

```sh
bk sync 1a2b3c
```

`bk sync [<id>]` backs up every configured pair, or one pair when an id prefix
is supplied. The first sync initializes the target with the entry id; later
syncs verify that id, so a wrong or replaced target is never written to.
The first sync of a target also creates that target's encryption keyring from
your password and stores it in `BK_BACKUP.json`; later syncs reuse the target's
stored keyring. Targets that aren't present (e.g. an unplugged drive) are
skipped. Each sync appends a new, verified bundle; existing versions are never
overwritten.

It prints one headerless, tab-separated record per pair: `id`, status, and a
message. The status uses the same vocabulary as `bk status` — a reached target
is reported online (`SYNCED_ONLINE`), an absent one falls back to its cached
offline verdict (`SYNCED_OFFLINE` / `STALE_OFFLINE`), and a real failure is
`ERROR` with the detail in the message column. A run with any `ERROR` exits
non-zero.

See the state of every configured backup:

```sh
bk status
```

This prints headerless, tab-separated records (one per backup) for easy
`cut`/`awk`/`read` — columns are id, state, source, target, last sync. The state
is one of:

- `SYNCED` up to date · `STALE` out of date · `NEW` never synced · `ERROR` error
- `SYNCED`/`STALE` carry an `_ONLINE` / `_OFFLINE` suffix: `_OFFLINE` means the
  target was absent, so the verdict is inferred from the last sync rather than
  verified

So a drive you synced and unplugged shows `SYNCED_OFFLINE` (your work is safe as
far as we know), turning `STALE_OFFLINE` once you make new commits (plug in to
back up). When the target is present its own state is authoritative.

Remove a configured entry. The backup data on the target is left alone:

```sh
bk rm 1a2b3c
```

If exactly one backup is configured, `bk rm` removes that entry. With zero or
multiple entries, pass an id from `bk status`.

Restore the latest version into a new directory:

```sh
bk restore /Volumes/usb/my-repo ~/code/restored
```

## Config

`bk add` writes to `~/.config/bk/config.json` (honoring `XDG_CONFIG_HOME`, or
`BK_CONFIG=/path` for an explicit file, set per-invocation if needed). In
simplified form:

```json
{
  "sync": [
    {
      "id": "...",
      "source": "/Users/me/code/my-repo",
      "target": "/Volumes/usb/my-repo",
      "backup": {
        "content_hash": "...",
        "synced_at": "..."
      }
    }
  ]
}
```

The entry id is assigned by `bk add` and written into the target sentinel on the
first successful sync. Encryption key material lives in the backup target, not
the config. The nested backup cache is filled on the first successful sync and
refreshed after later syncs.

## Layout

A backup is a plain directory using only appends and atomic overwrites:

```
backup/
  BK_BACKUP.json          sentinel + entry id + encrypted keyring
  latest.json             current version + refs fingerprint (path, refs_hash, synced_at)
  versions/
    bk-<timestamp>.bundle.age
    bk-<timestamp>.bundle.age.sha256
    ...
```

`latest.json` is written last and only ever points at a fully written,
verified bundle, so interrupted or concurrent syncs can't corrupt a backup.
It also records a fingerprint of the repo's refs, so a sync with no changes
is a fast no-op.

Note: bundles capture committed refs only — no working tree, stash, or
untracked files.
