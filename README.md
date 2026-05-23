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

## Usage

Back up a repository into a backup directory:

```sh
bk sync ~/code/my-repo backup
```

Each sync appends a new, verified bundle; existing versions are never
overwritten. Restore the latest version into a new directory:

```sh
bk restore backup ~/code/restored
```

## Layout

A backup is a plain directory using only appends and atomic overwrites:

```
backup/
  BK_BACKUP.json          sentinel + opaque id
  latest.txt              relative path of the current version
  versions/
    bk-<timestamp>-<rand>.bundle
    bk-<timestamp>-<rand>.bundle.sha256
    ...
```

`latest.txt` is updated last and only ever points at a fully written,
verified bundle, so interrupted or concurrent syncs can't corrupt a backup.

Note: bundles capture committed refs only — no working tree, stash, or
untracked files.
