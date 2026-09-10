package app

import (
	"context"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	claimstypes "github.com/ararat-network/ark/x/claims/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
)

// authSimModule samples auth parameter proposals within the SDK genesis bands so simulated
// transactions remain deliverable. See README.md, "Simulation fixtures", for the harness
// constraints.
type authSimModule struct {
	auth.AppModule
}

// ProposalMsgsX replaces the upstream registration rather than adding to it:
// the registry is keyed by message type, so one MsgUpdateParams factory wins.
func (m authSimModule) ProposalMsgsX(weights simsx.WeightSource, reg simsx.Registry) {
	reg.Add(weights.Get("msg_update_params", 100), authParamsFactory())
}

func authParamsFactory() simsx.SimMsgFactoryFn[*authtypes.MsgUpdateParams] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *authtypes.MsgUpdateParams) {
		r := testData.Rand()
		params := authtypes.DefaultParams()
		params.MaxMemoCharacters = r.Uint64InRange(100, 200)
		params.TxSigLimit = r.Uint64InRange(5, 12)
		params.TxSizeCostPerByte = r.Uint64InRange(5, 15)
		params.SigVerifyCostED25519 = r.Uint64InRange(500, 1000)
		params.SigVerifyCostSecp256k1 = r.Uint64InRange(500, 1000)

		return nil, &authtypes.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}

// simulatedFundBalance is what each protocol fund holds at genesis in a
// simulation. It is generous next to the mandate allowances drawn beside it, so
// a run exercises the guards rather than exhausting the balance immediately.
const simulatedFundBalance = int64(1_000_000_000_000_000)

// bankSimModule seeds Reserve and Insurance custody balances so simulation can exercise funded
// operations. Genesis supply increases by the same amount to preserve Bank's invariant.
type bankSimModule struct {
	bank.AppModule

	cdc codec.Codec
}

func (m bankSimModule) GenerateGenesisState(simState *module.SimulationState) {
	m.AppModule.GenerateGenesisState(simState)

	var bankGenesis banktypes.GenesisState
	m.cdc.MustUnmarshalJSON(simState.GenState[banktypes.ModuleName], &bankGenesis)

	seeded := sdk.NewCoins(sdk.NewCoin(simState.BondDenom, math.NewInt(simulatedFundBalance)))
	for _, fund := range []string{reservetypes.StrategicReserveName, claimstypes.InsuranceName} {
		bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
			Address: authtypes.NewModuleAddress(fund).String(),
			Coins:   seeded,
		})
		bankGenesis.Supply = bankGenesis.Supply.Add(seeded...)
	}

	simState.GenState[banktypes.ModuleName] = m.cdc.MustMarshalJSON(&bankGenesis)
}
