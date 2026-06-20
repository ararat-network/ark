package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/app/params"
	"noah/pkg/chain"
)

func init() {
	sdk.DefaultBondDenom = chain.MicroArkDenom

	// Set and seal config
	config := sdk.GetConfig()
	config.SetPurpose(params.Purpose)
	config.SetCoinType(params.CoinType)
	config.SetBech32PrefixForAccount(params.Bech32PrefixAccAddr, params.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(params.Bech32PrefixValAddr, params.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(params.Bech32PrefixConsAddr, params.Bech32PrefixConsPub)
	config.SetAddressVerifier(params.AddressVerifier)
	config.Seal()
}
