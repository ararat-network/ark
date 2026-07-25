package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"ark/app/params"
	"ark/pkg/chain"
)

func init() {
	sdk.DefaultBondDenom = chain.NoahBaseDenom
	sdk.DefaultPowerReduction = chain.NativeBaseAmount(1)
	govv1.DefaultMinDepositTokens = chain.NativeBaseAmount(10)
	govv1.DefaultMinExpeditedDepositTokens = govv1.DefaultMinDepositTokens.MulRaw(
		govv1.DefaultMinExpeditedDepositTokensRatio,
	)

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
