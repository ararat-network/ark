package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cometproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	abcicodec "ark/abci/codec"
	abcioracle "ark/abci/oracle"
	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/voteextension/types"
	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
)

const oracleBenchmarkValidatorCount = 100

var oracleBenchmarkPrices map[string]math.LegacyDec

type oracleBenchmarkFixture struct {
	app        *ArkApp
	valSet     *cmttypes.ValidatorSet
	consAddrs  []sdk.ConsAddress
	valAddrs   []sdk.ValAddress
	funder     sdk.AccAddress
	rewardPool sdk.Coins
}

type processVoteExtensionsBenchmarkCase struct {
	name        string
	targetCount int
	reportCount int
	missEvery   int
	allMissed   bool
}

type oracleSettlementBenchmarkCase struct {
	name                 string
	height               int64
	rewardWindow         uint64
	rewardDistribution   uint64
	slashWindow          uint64
	rewardDenomCount     int
	rewardValidatorCount int
	missValidatorCount   int
	slashEligible        bool
}

func BenchmarkOracleProcessVoteExtensions(b *testing.B) {
	fixture := newOracleBenchmarkFixture(b, oracleBenchmarkValidatorCount, 1, 2)
	baseCtx := fixture.app.NewNextBlockContext(cometproto.Header{
		Height: 3,
		Time:   time.Unix(3, 0).UTC(),
	})

	cases := []processVoteExtensionsBenchmarkCase{
		{name: "targets_8/reports_8/all_valid", targetCount: 8},
		{name: "targets_8/reports_8/ten_percent_missed", targetCount: 8, missEvery: 10},
		{name: "targets_8/reports_0/all_missed", targetCount: 8, allMissed: true},
		{name: "targets_256/reports_8/all_partial", targetCount: oracletypes.MaxVoteTargets, reportCount: 8},
		{name: "targets_256/reports_256/all_valid", targetCount: oracletypes.MaxVoteTargets},
	}

	for _, benchmarkCase := range cases {
		b.Run("validators_100/"+benchmarkCase.name, func(b *testing.B) {
			benchmarkProcessVoteExtensions(b, fixture, baseCtx, benchmarkCase)
		})
	}
}

func benchmarkProcessVoteExtensions(
	b *testing.B,
	fixture oracleBenchmarkFixture,
	parentCtx sdk.Context,
	benchmarkCase processVoteExtensionsBenchmarkCase,
) {
	b.Helper()

	baseCtx, _ := parentCtx.CacheContext()
	targets := benchmarkOracleTargets(benchmarkCase.targetCount)
	if err := fixture.app.OracleKeeper.VoteTargets.Set(baseCtx, oracletypes.VoteTargets{
		Denoms:  targets,
		Version: oracletypes.InitialVoteTargetVersion,
	}); err != nil {
		b.Fatal(err)
	}

	for _, valAddr := range fixture.valAddrs {
		if err := fixture.app.OracleKeeper.RewardWeight.Set(baseCtx, valAddr, math.NewInt(1_000_000)); err != nil {
			b.Fatal(err)
		}
		if err := fixture.app.OracleKeeper.MissCount.Set(baseCtx, valAddr, 1_000); err != nil {
			b.Fatal(err)
		}
	}

	voteExtensionCodec := abcicodec.NewVoteExtensionCodec()
	commitBz := benchmarkExtendedCommit(b, voteExtensionCodec, fixture.consAddrs, targets, benchmarkCase)
	req := &cometabci.RequestFinalizeBlock{
		Height: 3,
		Txs:    [][]byte{commitBz},
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(commitBz)))
	b.ReportMetric(oracleBenchmarkValidatorCount, "validators/op")
	b.ReportMetric(float64(benchmarkCase.targetCount), "targets/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx, _ := baseCtx.CacheContext()
		b.StartTimer()

		prices, err := abcioracle.ProcessVoteExtensions(ctx, fixture.app.OracleKeeper, voteExtensionCodec, req)
		if err != nil {
			b.Fatal(err)
		}
		oracleBenchmarkPrices = prices
	}
}

func BenchmarkOracleSettlement(b *testing.B) {
	cases := []oracleSettlementBenchmarkCase{
		{
			name:               "non_settlement",
			height:             4,
			rewardWindow:       10,
			rewardDistribution: 100,
			slashWindow:        20,
		},
		{
			name:                 "reward/validators_100/denoms_1",
			height:               9,
			rewardWindow:         10,
			rewardDistribution:   100,
			slashWindow:          20,
			rewardDenomCount:     1,
			rewardValidatorCount: oracleBenchmarkValidatorCount,
		},
		{
			name:                 "reward/validators_100/denoms_8",
			height:               9,
			rewardWindow:         10,
			rewardDistribution:   100,
			slashWindow:          20,
			rewardDenomCount:     8,
			rewardValidatorCount: oracleBenchmarkValidatorCount,
		},
		{
			name:               "slash/miss_records_10/eligible_0",
			height:             19,
			rewardWindow:       30,
			rewardDistribution: 100,
			slashWindow:        20,
			missValidatorCount: 10,
		},
		{
			name:               "slash/miss_records_100/eligible_100",
			height:             19,
			rewardWindow:       30,
			rewardDistribution: 100,
			slashWindow:        20,
			missValidatorCount: oracleBenchmarkValidatorCount,
			slashEligible:      true,
		},
		{
			name:                 "combined/validators_100/denoms_8",
			height:               9,
			rewardWindow:         10,
			rewardDistribution:   100,
			slashWindow:          10,
			rewardDenomCount:     8,
			rewardValidatorCount: oracleBenchmarkValidatorCount,
			missValidatorCount:   oracleBenchmarkValidatorCount,
			slashEligible:        true,
		},
	}

	for _, benchmarkCase := range cases {
		b.Run(benchmarkCase.name, func(b *testing.B) {
			benchmarkOracleSettlement(b, benchmarkCase)
		})
	}
}

func benchmarkOracleSettlement(b *testing.B, benchmarkCase oracleSettlementBenchmarkCase) {
	b.Helper()

	rewardDenomCount := benchmarkCase.rewardDenomCount
	if rewardDenomCount == 0 {
		rewardDenomCount = 1
	}
	fixture := newOracleBenchmarkFixture(
		b,
		oracleBenchmarkValidatorCount,
		rewardDenomCount,
		0,
	)
	baseCtx := fixture.app.NewNextBlockContext(cometproto.Header{
		Height: benchmarkCase.height,
		Time:   time.Unix(benchmarkCase.height, 0).UTC(),
	})

	params, err := fixture.app.OracleKeeper.Params.Get(baseCtx)
	if err != nil {
		b.Fatal(err)
	}
	params.RewardWindow = benchmarkCase.rewardWindow
	params.RewardDistributionWindow = benchmarkCase.rewardDistribution
	params.SlashWindow = benchmarkCase.slashWindow
	if err := fixture.app.OracleKeeper.Params.Set(baseCtx, params); err != nil {
		b.Fatal(err)
	}
	if err := fixture.app.OracleKeeper.Accounting.Set(baseCtx, oracletypes.NewAccounting(params)); err != nil {
		b.Fatal(err)
	}

	for _, valAddr := range fixture.valAddrs[:benchmarkCase.rewardValidatorCount] {
		if err := fixture.app.OracleKeeper.RewardWeight.Set(baseCtx, valAddr, math.NewInt(8)); err != nil {
			b.Fatal(err)
		}
	}
	for _, valAddr := range fixture.valAddrs[:benchmarkCase.missValidatorCount] {
		missCount := uint64(1)
		if benchmarkCase.slashEligible {
			missCount = benchmarkCase.slashWindow
		}
		if err := fixture.app.OracleKeeper.MissCount.Set(baseCtx, valAddr, missCount); err != nil {
			b.Fatal(err)
		}
	}
	if benchmarkCase.rewardValidatorCount > 0 {
		if err := fixture.app.BankKeeper.SendCoinsFromAccountToModule(
			baseCtx,
			fixture.funder,
			oracletypes.ModuleName,
			fixture.rewardPool,
		); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ReportMetric(float64(benchmarkCase.rewardValidatorCount), "reward_validators/op")
	b.ReportMetric(float64(benchmarkCase.missValidatorCount), "miss_records/op")
	b.ReportMetric(float64(benchmarkCase.rewardDenomCount), "reward_denoms/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx, _ := baseCtx.CacheContext()
		b.StartTimer()

		if err := fixture.app.OracleKeeper.EndBlocker(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOracleFinalizeAndCommit(b *testing.B) {
	b.Run("empty/validators_100", func(b *testing.B) {
		benchmarkOracleFinalizeAndCommit(b, false)
	})
	b.Run("oracle/validators_100/targets_8", func(b *testing.B) {
		benchmarkOracleFinalizeAndCommit(b, true)
	})
}

func benchmarkOracleFinalizeAndCommit(b *testing.B, withOracle bool) {
	b.Helper()

	var voteExtensionsEnableHeight int64
	if withOracle {
		voteExtensionsEnableHeight = 2
	}
	fixture := newOracleBenchmarkFixture(
		b,
		oracleBenchmarkValidatorCount,
		1,
		voteExtensionsEnableHeight,
	)

	commitInfo := cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, len(fixture.consAddrs))}
	for i, consAddr := range fixture.consAddrs {
		commitInfo.Votes[i] = cometabci.VoteInfo{
			Validator:   cometabci.Validator{Address: consAddr, Power: 1},
			BlockIdFlag: cometproto.BlockIDFlagCommit,
		}
	}

	var txs [][]byte
	if withOracle {
		targets := oracletypes.NewVoteTargets(oracletypes.DefaultParams())
		commitBz := benchmarkExtendedCommit(
			b,
			abcicodec.NewVoteExtensionCodec(),
			fixture.consAddrs,
			targets.Denoms,
			processVoteExtensionsBenchmarkCase{targetCount: len(targets.Denoms)},
		)
		txs = [][]byte{commitBz}
		b.SetBytes(int64(len(commitBz)))
	}

	b.ReportAllocs()
	b.ReportMetric(oracleBenchmarkValidatorCount, "validators/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		height := fixture.app.LastBlockHeight() + 1
		_, err := fixture.app.FinalizeBlock(&cometabci.RequestFinalizeBlock{
			Height:             height,
			Time:               time.Unix(height, 0).UTC(),
			Hash:               fixture.app.LastCommitID().Hash,
			NextValidatorsHash: fixture.valSet.Hash(),
			DecidedLastCommit:  commitInfo,
			Txs:                txs,
		})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := fixture.app.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func newOracleBenchmarkFixture(
	tb testing.TB,
	validatorCount,
	rewardDenomCount int,
	voteExtensionsEnableHeight int64,
) oracleBenchmarkFixture {
	tb.Helper()

	validators := make([]*cmttypes.Validator, validatorCount)
	consAddrs := make([]sdk.ConsAddress, validatorCount)
	for i := range validators {
		privVal := mock.NewPV()
		pubKey, err := privVal.GetPubKey()
		if err != nil {
			tb.Fatal(err)
		}
		validators[i] = cmttypes.NewValidator(pubKey, 1)
		consAddrs[i] = sdk.ConsAddress(validators[i].Address)
	}
	valSet := cmttypes.NewValidatorSet(validators)

	funderKey := secp256k1.GenPrivKey()
	funder := authtypes.NewBaseAccount(funderKey.PubKey().Address().Bytes(), funderKey.PubKey(), 0, 0)
	rewardPool := benchmarkRewardPool(rewardDenomCount)
	genesisBalance := rewardPool.Add(
		sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(1_000_000_000_000_000_000)),
	)

	arkApp := NewArkApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(tb.TempDir()),
	)
	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
		valSet,
		[]authtypes.GenesisAccount{funder},
		banktypes.Balance{Address: funder.GetAddress().String(), Coins: genesisBalance},
	)
	if err != nil {
		tb.Fatal(err)
	}
	// GenesisStateWithValSet accounts for every validator in total supply but
	// seeds only one bond amount in the bonded pool. Correct the pool balance for
	// this multi-validator benchmark fixture before InitChain validates supply.
	var bankGenesis banktypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[banktypes.ModuleName], &bankGenesis)
	bondedPoolAddress := authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String()
	for i := range bankGenesis.Balances {
		if bankGenesis.Balances[i].Address != bondedPoolAddress {
			continue
		}
		bankGenesis.Balances[i].Coins = sdk.NewCoins(sdk.NewCoin(
			sdk.DefaultBondDenom,
			sdk.DefaultPowerReduction.MulRaw(int64(validatorCount)),
		))
		break
	}
	genesisState[banktypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&bankGenesis)

	var slashingGenesis slashingtypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[slashingtypes.ModuleName], &slashingGenesis)
	slashingGenesis.SigningInfos = make([]slashingtypes.SigningInfo, len(consAddrs))
	for i, consAddr := range consAddrs {
		slashingGenesis.SigningInfos[i] = slashingtypes.SigningInfo{
			Address: consAddr.String(),
			ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
				consAddr,
				1,
				0,
				time.Unix(0, 0).UTC(),
				false,
				0,
			),
		}
	}
	genesisState[slashingtypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&slashingGenesis)
	stateBytes, err := json.Marshal(genesisState)
	if err != nil {
		tb.Fatal(err)
	}

	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cometproto.ConsensusParams)
	if consensusParams.Abci == nil {
		consensusParams.Abci = &cometproto.ABCIParams{}
	}
	consensusParams.Abci.VoteExtensionsEnableHeight = voteExtensionsEnableHeight
	if _, err := arkApp.InitChain(&cometabci.RequestInitChain{
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	}); err != nil {
		tb.Fatal(err)
	}
	if _, err := arkApp.FinalizeBlock(&cometabci.RequestFinalizeBlock{
		Height:             1,
		Time:               time.Unix(1, 0).UTC(),
		NextValidatorsHash: valSet.Hash(),
	}); err != nil {
		tb.Fatal(err)
	}
	ctx := arkApp.NewContextLegacy(false, cometproto.Header{Height: 2})
	valAddrs := make([]sdk.ValAddress, len(consAddrs))
	for i, consAddr := range consAddrs {
		validator, err := arkApp.StakingKeeper.ValidatorByConsAddr(ctx, consAddr)
		if err != nil {
			tb.Fatal(err)
		}
		valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
		if err != nil {
			tb.Fatal(err)
		}
		valAddrs[i] = valAddr
	}
	if _, err := arkApp.Commit(); err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		if err := arkApp.Close(); err != nil {
			tb.Errorf("closing benchmark app: %v", err)
		}
	})

	return oracleBenchmarkFixture{
		app:        arkApp,
		valSet:     valSet,
		consAddrs:  consAddrs,
		valAddrs:   valAddrs,
		funder:     funder.GetAddress(),
		rewardPool: rewardPool,
	}
}

func benchmarkExtendedCommit(
	tb testing.TB,
	voteExtensionCodec *abcicodec.VoteExtensionCodec,
	consAddrs []sdk.ConsAddress,
	targets []string,
	benchmarkCase processVoteExtensionsBenchmarkCase,
) []byte {
	tb.Helper()

	reportCount := benchmarkCase.reportCount
	if reportCount == 0 {
		reportCount = len(targets)
	}
	reportedTargets := targets[:reportCount]
	fullVoteExtension := benchmarkVoteExtension(tb, voteExtensionCodec, reportedTargets)
	partialTargets := reportedTargets
	if len(partialTargets) > 0 {
		partialTargets = partialTargets[:len(partialTargets)-1]
	}
	partialVoteExtension := benchmarkVoteExtension(tb, voteExtensionCodec, partialTargets)

	votes := make([]cometabci.ExtendedVoteInfo, len(consAddrs))
	for i, consAddr := range consAddrs {
		voteExtension := fullVoteExtension
		switch {
		case benchmarkCase.allMissed:
			voteExtension = nil
		case benchmarkCase.missEvery > 0 && i%benchmarkCase.missEvery == 0:
			voteExtension = partialVoteExtension
		}
		votes[i] = cometabci.ExtendedVoteInfo{
			Validator:     cometabci.Validator{Address: consAddr, Power: 1},
			VoteExtension: voteExtension,
			BlockIdFlag:   cometproto.BlockIDFlagCommit,
		}
	}

	encoded, err := abcicodec.EncodeExtendedCommit(cometabci.ExtendedCommitInfo{Votes: votes})
	if err != nil {
		tb.Fatal(err)
	}
	return encoded
}

func benchmarkVoteExtension(
	tb testing.TB,
	voteExtensionCodec *abcicodec.VoteExtensionCodec,
	targets []string,
) []byte {
	tb.Helper()

	rates := make(map[string][]byte, len(targets))
	for i, denom := range targets {
		rate, err := oracleencoding.EncodeRate(math.LegacyNewDec(int64(i + 100)))
		if err != nil {
			tb.Fatal(err)
		}
		rates[denom] = rate
	}
	encoded, err := voteExtensionCodec.Encode(vetypes.OracleVoteExtension{
		Rates:         rates,
		TargetVersion: oracletypes.InitialVoteTargetVersion,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return encoded
}

func benchmarkOracleTargets(count int) []string {
	targets := make([]string, count)
	for i := range targets {
		targets[i] = fmt.Sprintf("uasset%03d", i)
	}
	return targets
}

func benchmarkRewardPool(count int) sdk.Coins {
	if count <= 0 {
		return nil
	}

	rewards := make(sdk.Coins, 0, count)
	for i := range count {
		denom := chain.MicroNoahDenom
		if i > 0 {
			denom = fmt.Sprintf("ureward%03d", i)
		}
		rewards = append(rewards, sdk.NewCoin(denom, math.NewInt(1_000_000_000)))
	}
	return rewards.Sort()
}
