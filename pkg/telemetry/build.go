package telemetry

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

// BuildVersion identifies this process's build. It is the SDK version ldflag
// when the build set one, otherwise what the Go toolchain embedded: the module
// version for installed builds, the VCS revision for source-tree ones. Both
// binaries carry it on target_info as service.version, and the sidecar
// reports the same string over its RPC.
func BuildVersion() string {
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
