package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	abcicodec "github.com/ararat-network/ark/abci/codec"
	abcioracle "github.com/ararat-network/ark/abci/oracle"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	chain "github.com/ararat-network/ark/pkg/chain"
	arkencoding "github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

const oracleBenchmarkValidatorCount = 100

var oracleBenchmarkPrices map[string]math.LegacyDec

type oracleBenchmarkFixture struct {
	app        *app.ArkApp
	validators apptestutil.Validators
	consAddrs  []sdk.ConsAddress
	valAddrs   []sdk.ValAddress
	funder     sdk.AccAddress
	rewardPool sdk.Coins
}

type processVoteExtensionsBenchmarkCase struct {
	name         string
	targetCount  int
	reportCount  int
	partialEvery int
	allAbsent    bool
}

type oracleSettlementBenchmarkCase struct {
	name                 string
	height               int64
	rewardWindow         uint64
	rewardDistribution   uint64
	attendanceWindow     uint64
	rewardDenomCount     int
	rewardValidatorCount int
	absentValidatorCount int
	jailEligible         bool
}

func BenchmarkOracleProcessVoteExtensions(b *testing.B) {
	fixture := newOracleBenchmarkFixture(b, oracleBenchmarkValidatorCount, 1, 2)
	baseCtx := fixture.app.NewNextBlockContext(cmtproto.Header{
		Height: 3,
		Time:   time.Unix(3, 0).UTC(),
	})

	cases := []processVoteExtensionsBenchmarkCase{
		{name: "targets_8/reports_8/all_valid", targetCount: 8},
		{name: "targets_8/reports_8/ten_percent_partial", targetCount: 8, partialEvery: 10},
		{name: "targets_8/reports_0/all_absent", targetCount: 8, allAbsent: true},
		{name: "targets_256/reports_8/all_partial", targetCount: oracletypes.MaxFeeds, reportCount: 8},
		{name: "targets_256/reports_256/all_valid", targetCount: oracletypes.MaxFeeds},
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
	if err := fixture.app.OracleKeeper.Feeds.Set(baseCtx, oracletypes.Feeds{
		Denoms:  targets,
		Version: oracletypes.InitialFeedVersion,
	}); err != nil {
		b.Fatal(err)
	}

	for _, valAddr := range fixture.valAddrs {
		if err := fixture.app.OracleKeeper.RewardWeight.Set(baseCtx, valAddr, math.NewInt(1_000_000)); err != nil {
			b.Fatal(err)
		}
		// A validator with this much accumulated reward weight has a long
		// history of actually participating, so model it as fully attended.
		if err := fixture.app.OracleKeeper.Attendance.Set(baseCtx, valAddr, oracletypes.Attendance{
			EligibleBlocks: 1_000,
			AttendedBlocks: 1_000,
		}); err != nil {
			b.Fatal(err)
		}
	}
	commitBz := benchmarkExtendedCommit(b, fixture.consAddrs, targets, benchmarkCase)
	req := &cmtabci.RequestFinalizeBlock{
		Height:            3,
		Txs:               [][]byte{commitBz},
		DecidedLastCommit: cmtabci.CommitInfo{Votes: make([]cmtabci.VoteInfo, len(fixture.consAddrs))},
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

		prices, err := abcioracle.ProcessVoteExtensions(ctx, fixture.app.OracleKeeper, req)
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
			attendanceWindow:   20,
		},
		{
			name:                 "reward/validators_100/denoms_1",
			height:               9,
			rewardWindow:         10,
			rewardDistribution:   100,
			attendanceWindow:     20,
			rewardDenomCount:     1,
			rewardValidatorCount: oracleBenchmarkValidatorCount,
		},
		{
			name:                 "reward/validators_100/denoms_8",
			height:               9,
			rewardWindow:         10,
			rewardDistribution:   100,
			attendanceWindow:     20,
			rewardDenomCount:     8,
			rewardValidatorCount: oracleBenchmarkValidatorCount,
		},
		{
			name:                 "attendance/records_10/jailed_0",
			height:               19,
			rewardWindow:         30,
			rewardDistribution:   100,
			attendanceWindow:     20,
			absentValidatorCount: 10,
		},
		{
			name:                 "attendance/records_100/jailed_100",
			height:               19,
			rewardWindow:         30,
			rewardDistribution:   100,
			attendanceWindow:     20,
			absentValidatorCount: oracleBenchmarkValidatorCount,
			jailEligible:         true,
		},
		{
			name:                 "combined/validators_100/denoms_8",
			height:               9,
			rewardWindow:         10,
			rewardDistribution:   100,
			attendanceWindow:     10,
			rewardDenomCount:     8,
			rewardValidatorCount: oracleBenchmarkValidatorCount,
			absentValidatorCount: oracleBenchmarkValidatorCount,
			jailEligible:         true,
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
	baseCtx := fixture.app.NewNextBlockContext(cmtproto.Header{
		Height: benchmarkCase.height,
		Time:   time.Unix(benchmarkCase.height, 0).UTC(),
	})

	params, err := fixture.app.OracleKeeper.Params.Get(baseCtx)
	if err != nil {
		b.Fatal(err)
	}
	params.RewardWindow = benchmarkCase.rewardWindow
	params.RewardDistributionWindow = benchmarkCase.rewardDistribution
	params.AttendanceWindow = benchmarkCase.attendanceWindow
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
	for _, valAddr := range fixture.valAddrs[:benchmarkCase.absentValidatorCount] {
		// Every seeded validator was eligible for the whole window. A
		// jail-eligible case attends none of it; the others attend all but one
		// block, which stays far above the minimum attendance ratio.
		absentBlocks := uint64(1)
		if benchmarkCase.jailEligible {
			absentBlocks = benchmarkCase.attendanceWindow
		}
		attendance := oracletypes.Attendance{
			EligibleBlocks: benchmarkCase.attendanceWindow,
			AttendedBlocks: benchmarkCase.attendanceWindow - absentBlocks,
		}
		if err := fixture.app.OracleKeeper.Attendance.Set(baseCtx, valAddr, attendance); err != nil {
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
	b.ReportMetric(float64(benchmarkCase.absentValidatorCount), "attendance_records/op")
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

	commitInfo := cmtabci.CommitInfo{Votes: make([]cmtabci.VoteInfo, len(fixture.consAddrs))}
	for i, consAddr := range fixture.consAddrs {
		commitInfo.Votes[i] = cmtabci.VoteInfo{
			Validator:   cmtabci.Validator{Address: consAddr, Power: 1},
			BlockIdFlag: cmtproto.BlockIDFlagCommit,
		}
	}

	var txs [][]byte
	if withOracle {
		feeds := oracletypes.DefaultFeeds()
		commitBz := benchmarkExtendedCommit(
			b,
			fixture.consAddrs,
			feeds.Denoms,
			processVoteExtensionsBenchmarkCase{targetCount: len(feeds.Denoms)},
		)
		txs = [][]byte{commitBz}
		b.SetBytes(int64(len(commitBz)))
	}

	b.ReportAllocs()
	b.ReportMetric(oracleBenchmarkValidatorCount, "validators/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		height := fixture.app.LastBlockHeight() + 1
		_, err := fixture.app.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
			Height:             height,
			Time:               time.Unix(height, 0).UTC(),
			Hash:               fixture.app.LastCommitID().Hash,
			NextValidatorsHash: fixture.validators.Set.Hash(),
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

	validators := apptestutil.NewValidators(tb, validatorCount)
	consAddrs := validators.ConsAddresses()

	rewardPool := benchmarkRewardPool(rewardDenomCount)
	funder := apptestutil.NewFunder(tb, rewardPool.Add(
		sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(1_000_000_000_000_000_000)),
	))

	arkApp := app.NewArkApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(tb.TempDir()),
	)
	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
		validators.Set,
		funder.Accounts(),
		funder.Balance,
	)
	if err != nil {
		tb.Fatal(err)
	}
	apptestutil.CorrectBondedPool(tb, arkApp.AppCodec(), genesisState, validatorCount)
	apptestutil.SeedSigningInfos(tb, arkApp.AppCodec(), genesisState, consAddrs, 1)
	stateBytes, err := json.Marshal(genesisState)
	if err != nil {
		tb.Fatal(err)
	}

	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	if consensusParams.Abci == nil {
		consensusParams.Abci = &cmtproto.ABCIParams{}
	}
	consensusParams.Abci.VoteExtensionsEnableHeight = voteExtensionsEnableHeight
	if _, err := arkApp.InitChain(&cmtabci.RequestInitChain{
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	}); err != nil {
		tb.Fatal(err)
	}
	if _, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             1,
		Time:               time.Unix(1, 0).UTC(),
		NextValidatorsHash: validators.Set.Hash(),
	}); err != nil {
		tb.Fatal(err)
	}
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 2})
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
		validators: validators,
		consAddrs:  consAddrs,
		valAddrs:   valAddrs,
		funder:     funder.Address(),
		rewardPool: rewardPool,
	}
}

func benchmarkExtendedCommit(
	tb testing.TB,
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
	fullVoteExtension := benchmarkVoteExtension(tb, reportedTargets)
	partialTargets := reportedTargets
	if len(partialTargets) > 0 {
		partialTargets = partialTargets[:len(partialTargets)-1]
	}
	partialVoteExtension := benchmarkVoteExtension(tb, partialTargets)

	votes := make([]cmtabci.ExtendedVoteInfo, len(consAddrs))
	for i, consAddr := range consAddrs {
		voteExtension := fullVoteExtension
		switch {
		case benchmarkCase.allAbsent:
			voteExtension = nil
		case benchmarkCase.partialEvery > 0 && i%benchmarkCase.partialEvery == 0:
			voteExtension = partialVoteExtension
		}
		votes[i] = cmtabci.ExtendedVoteInfo{
			Validator:     cmtabci.Validator{Address: consAddr, Power: 1},
			VoteExtension: voteExtension,
			BlockIdFlag:   cmtproto.BlockIDFlagCommit,
		}
	}

	encoded, err := abcicodec.EncodeExtendedCommit(cmtabci.ExtendedCommitInfo{Votes: votes})
	if err != nil {
		tb.Fatal(err)
	}
	return encoded
}

func benchmarkVoteExtension(
	tb testing.TB,
	targets []string,
) []byte {
	tb.Helper()

	rates := make(map[string][]byte, len(targets))
	for i, denom := range targets {
		rate, err := arkencoding.EncodeCompactLegacyDec(math.LegacyNewDec(int64(i + 100)))
		if err != nil {
			tb.Fatal(err)
		}
		rates[denom] = rate
	}
	encoded, err := abcicodec.EncodeVoteExtension(vetypes.OracleVoteExtension{
		Rates:         rates,
		TargetVersion: oracletypes.InitialFeedVersion,
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
		denom := chain.NoahBaseDenom
		if i > 0 {
			denom = fmt.Sprintf("ureward%03d", i)
		}
		rewards = append(rewards, sdk.NewCoin(denom, math.NewInt(1_000_000_000)))
	}
	return rewards.Sort()
}
