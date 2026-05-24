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

`bk sync` backs up every configured pair. The first sync initializes the
target and records its id; later syncs verify that id, so a wrong or replaced
target is never written to. Targets that aren't present (e.g. an unplugged
drive) are skipped. Each sync appends a new, verified bundle; existing
versions are never overwritten.

See the state of every configured backup:

```sh
bk status
```

Or just run `bk` with no arguments for a live dashboard. Each backup shows a
colored dot and its last sync time, where the color is *currency* and the
brightness is *connection*:

- green = synced, yellow = out of date, muted = never synced, red = error
- a dim dot means the target is offline (e.g. an unplugged drive)

So a drive you synced and unplugged shows dim green ("synced · offline") — your
work is safe — and turns dim yellow once you make new commits ("out of date ·
offline", i.e. plug in to back up). Currency while offline is inferred from the
last sync recorded in the config; when the target is present its own state is
authoritative. Red is reserved for real errors (id mismatch, not a backup,
unreadable source). It re-checks continuously, so plugging in a drive or adding
an entry elsewhere shows up on its own.

By default it only *shows* status. Press `a` to toggle auto-sync, which keeps
out-of-date backups synced automatically (turning their dots green); `q` quits.
(When output isn't a terminal, `bk` prints a one-shot status snapshot.)

Restore the latest version into a new directory:

```sh
bk restore /Volumes/usb/my-repo ~/code/restored
```

## Config

`bk add` writes to `~/.config/bk/config.json` (honoring `XDG_CONFIG_HOME`, or
`BK_CONFIG=/path` for an explicit file, set per-invocation if needed):

```json
{
  "sync": [
    { "source": "/Users/me/code/my-repo", "target": "/Volumes/usb/my-repo", "id": "..." }
  ]
}
```

`id` is empty until the first sync, which fills it in from the target.

## Layout

A backup is a plain directory using only appends and atomic overwrites:

```
backup/
  BK_BACKUP.json          sentinel + opaque id
  latest.json             current version + refs fingerprint (path, refs_hash, synced_at)
  versions/
    bk-<timestamp>-<rand>.bundle
    bk-<timestamp>-<rand>.bundle.sha256
    ...
```

`latest.json` is written last and only ever points at a fully written,
verified bundle, so interrupted or concurrent syncs can't corrupt a backup.
It also records a fingerprint of the repo's refs, so a sync with no changes
is a fast no-op.

Note: bundles capture committed refs only — no working tree, stash, or
untracked files.
