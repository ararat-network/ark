package app

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app/params"
	"github.com/ararat-network/ark/pkg/chain"
)

func init() {
	sdk.DefaultBondDenom = chain.NoahBaseDenom
	sdk.DefaultPowerReduction = chain.NativeBaseAmount(1)
	govv1.DefaultMinDepositTokens = chain.NativeBaseAmount(10)
	govv1.DefaultMinExpeditedDepositTokens = govv1.DefaultMinDepositTokens.MulRaw(
		govv1.DefaultMinExpeditedDepositTokensRatio,
	)
	// A proposal's initial deposit must reach this share of MinDeposit, or the
	// submission is rejected outright; matches the Cosmos Hub's 10%.
	govv1.DefaultMinInitialDepositRatio = math.LegacyNewDecWithPrec(1, 1)

	// sdk.KeyringServiceName reads version.Name, and on the os, pass, and
	// kwallet backends that name is the credential store's service label. Unset,
	// it falls back to "cosmos" and files Ark keys under another chain's name.
	// Set here rather than only through ldflags because `go build` and `go test`
	// pass none, and a service rename later leaves stored keys unreachable.
	version.Name = Name
	version.AppName = Name + "d"

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
