package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestSortBeneficiaries(t *testing.T) {
	record := func(b byte) types.Beneficiary { return types.Beneficiary{Address: testAddress(b)} }
	list := []types.Beneficiary{record(3), record(1), record(2)}
	types.SortBeneficiaries(list)
	require.Equal(t, []types.Beneficiary{record(1), record(2), record(3)}, list)
}
