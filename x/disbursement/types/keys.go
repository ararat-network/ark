package types

import (
	"fmt"
	"slices"

	"cosmossdk.io/collections"
)

const (
	ModuleName = "disbursement"
	StoreKey   = ModuleName
	// ContributorsName holds the contributor pool, ownership and compensation escrow in every
	// denomination, and deposits.
	ContributorsName = "disbursement_contributors"
	// MembersName holds the member pool and every member grant's escrow, NOAH only. It has no exit.
	MembersName = "disbursement_members"
)

// Store prefixes; the gaps are retired prefixes, never reused.
var (
	ParamsKey                 = collections.NewPrefix(0)
	GrantsKey                 = collections.NewPrefix(1)
	BeneficiariesKey          = collections.NewPrefix(2)
	MembersKey                = collections.NewPrefix(3)
	BooksKey                  = collections.NewPrefix(4)
	BeneficiaryJournalKey     = collections.NewPrefix(5)
	GrantsMandateKey          = collections.NewPrefix(6)
	GrantJournalKey           = collections.NewPrefix(7)
	NextGrantIDKey            = collections.NewPrefix(8)
	NextGrantEntryIDKey       = collections.NewPrefix(9)
	GrantsByBeneficiaryKey    = collections.NewPrefix(10)
	NextBeneficiaryEntryIDKey = collections.NewPrefix(11)
	CommitteeTermGrantsKey    = collections.NewPrefix(12)
	DueKey                    = collections.NewPrefix(13)
	CompensationUsedKey       = collections.NewPrefix(14)
	RegistrarMandateKey       = collections.NewPrefix(16)
	RegistrarTermGrantsKey    = collections.NewPrefix(17)
)

// PoolAccountNames lists the module's pool accounts in storage-key order.
func PoolAccountNames() []string {
	return []string{ContributorsName, MembersName}
}

// Account names the account a grant kind draws on and pays from.
func Account(kind GrantKind) string {
	if kind == GrantKind_GRANT_KIND_MEMBER {
		return MembersName
	}
	return ContributorsName
}

// ValidateAccount checks a pool names one of the module's accounts.
func ValidateAccount(pool string) error {
	if !slices.Contains(PoolAccountNames(), pool) {
		return fmt.Errorf("%q is not a disbursement pool", pool)
	}
	return nil
}
