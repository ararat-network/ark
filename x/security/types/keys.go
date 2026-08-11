package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the security module
	ModuleName = "security"
	// StoreKey is the string store representation
	StoreKey = ModuleName
)

// Keys for security store
var (
	SecurityMandateKey = collections.NewPrefix(0)
	CommitteePlanKey   = collections.NewPrefix(1)
)
