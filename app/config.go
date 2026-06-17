package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/pkg/types"
)

func init() {
	sdk.DefaultBondDenom = core.MicroArkDenom

	// Set and seal config
	config := sdk.GetConfig()
	config.SetPurpose(core.Purpose)
	config.SetCoinType(core.CoinType)
	config.SetBech32PrefixForAccount(core.Bech32PrefixAccAddr, core.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(core.Bech32PrefixValAddr, core.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(core.Bech32PrefixConsAddr, core.Bech32PrefixConsPub)
	config.SetAddressVerifier(core.AddressVerifier)
	config.Seal()
}
