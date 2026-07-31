package sidecar

import (
	"runtime/debug"
	"strings"

	"github.com/cosmos/cosmos-sdk/version"
)

const (
	// unknownVersion is reported when neither ldflags nor embedded build
	// information identify the build.
	unknownVersion = "unknown"

	// shortRevisionLength is the abbreviated commit length reported for builds
	// identified only by their VCS revision.
	shortRevisionLength = 12
)

// Version returns the build version reported by the price RPC and the oracle
// CLI.
//
// The oracle binary is not built with the SDK's version ldflags, so an unset
// version.Version falls back to the build information the Go toolchain embeds:
// the module version for installed builds, and the VCS revision for builds made
// from a source tree.
func Version() string {
	if v := strings.TrimSpace(version.Version); v != "" {
		return v
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unknownVersion
	}
	// Source-tree builds report "(devel)" as the module version, which
	// identifies nothing; their revision is recorded in the build settings.
	if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
		return v
	}

	return revisionFromBuildInfo(info)
}

// revisionFromBuildInfo returns the abbreviated VCS revision recorded in a
// source-tree build, marking builds made from a modified worktree.
func revisionFromBuildInfo(info *debug.BuildInfo) string {
	var (
		revision string
		modified bool
	)
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if revision == "" {
		return unknownVersion
	}
	if len(revision) > shortRevisionLength {
		revision = revision[:shortRevisionLength]
	}
	if modified {
		return revision + "-dirty"
	}

	return revision
}
