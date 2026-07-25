package types

const (
	// MaxOracleTargets bounds the target set carried through vote extensions.
	MaxOracleTargets = 256
	// InitialOracleTargetVersion is the first valid target epoch version.
	InitialOracleTargetVersion uint64 = 1
	// OracleTargetActivationDelayBlocks preserves the existing two-height target
	// transition boundary.
	OracleTargetActivationDelayBlocks int64 = 2
)
