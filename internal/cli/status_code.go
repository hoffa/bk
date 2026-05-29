package cli

import "github.com/hoffa/bk/internal/bk"

// statusCode is the machine-readable code for a state + presence. The _ONLINE /
// _OFFLINE suffix records whether the verdict was confirmed against a present
// target or inferred from the config's cache while the target was absent.
func statusCode(s bk.State, present bool) string {
	suffix := "_OFFLINE"
	if present {
		suffix = "_ONLINE"
	}

	switch s {
	case bk.StateSynced:
		return "SYNCED" + suffix
	case bk.StateStale:
		return "STALE" + suffix
	case bk.StateUnsynced:
		return "NEW"
	case bk.StateError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
