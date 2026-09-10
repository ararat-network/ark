package integration

import (
	"context"
	"encoding/json"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	cryptomultisig "github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	claimskeeper "github.com/ararat-network/ark/x/claims/keeper"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

const treasuryMultisigChainID = "ark-treasury-multisig-test"

// The declared multisig fee equals the default gas requirement. Insufficient-funds cases reduce
// custody, since excess declaration alone is not charged.
const (
	multisigGasLimit            = 2_000_000
	multisigFeeAmount           = 200_000_000_000_000_000
	multisigCommitteeFeeBalance = 1_000_000_000_000_000_000
	multisigStarvedFeeBalance   = 100_000_000_000_000_000
)

type treasuryMultisigMemberSignature struct {
	memberIndex int
	privateKey  cryptotypes.PrivKey
}

func TestTreasuryClaimsCommitteeLegacyAminoMultisig(t *testing.T) {
	members := []cryptotypes.PrivKey{
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
	}
	memberPubKeys := make([]cryptotypes.PubKey, len(members))
	for i, member := range members {
		memberPubKeys[i] = member.PubKey()
	}
	committeePubKey := kmultisig.NewLegacyAminoPubKey(2, memberPubKeys)
	committee := sdk.AccAddress(committeePubKey.Address())
	wrongSigner := secp256k1.GenPrivKey()

	tests := []struct {
		name                 string
		signatures           []treasuryMultisigMemberSignature
		omitCommitteeAccount bool
		sequenceOffset       uint64
		// committeeFeeBalance is what the committee holds in the fee
		// denomination; zero funds it the default, several times the fee.
		committeeFeeBalance int64
		// wantLogContains pins why a rejected transaction was rejected, where
		// the signatures alone would not say.
		wantLogContains string
		wantClaim       bool
	}{
		{
			name: "two of three member signatures succeed",
			signatures: []treasuryMultisigMemberSignature{
				{memberIndex: 0, privateKey: members[0]},
				{memberIndex: 2, privateKey: members[2]},
			},
			wantClaim: true,
		},
		{
			name: "one member signature is insufficient",
			signatures: []treasuryMultisigMemberSignature{
				{memberIndex: 0, privateKey: members[0]},
			},
		},
		{
			name: "signature from a non-member fails",
			signatures: []treasuryMultisigMemberSignature{
				{memberIndex: 0, privateKey: members[0]},
				{memberIndex: 1, privateKey: wrongSigner},
			},
		},
		{
			name:                 "nonexistent committee account fails",
			omitCommitteeAccount: true,
			signatures: []treasuryMultisigMemberSignature{
				{memberIndex: 0, privateKey: members[0]},
				{memberIndex: 2, privateKey: members[2]},
			},
		},
		{
			name:           "wrong sequence fails",
			sequenceOffset: 1,
			signatures: []treasuryMultisigMemberSignature{
				{memberIndex: 0, privateKey: members[0]},
				{memberIndex: 2, privateKey: members[2]},
			},
		},
		{
			// The committee holds less than the base fee its gas limit
			// requires, so the deduction fails on funds.
			name:                "base fee above committee balance fails",
			committeeFeeBalance: multisigStarvedFeeBalance,
			wantLogContains:     "insufficient funds",
			signatures: []treasuryMultisigMemberSignature{
				{memberIndex: 0, privateKey: members[0]},
				{memberIndex: 2, privateKey: members[2]},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			committeeFeeBalance := int64(multisigCommitteeFeeBalance)
			if test.committeeFeeBalance > 0 {
				committeeFeeBalance = test.committeeFeeBalance
			}
			arkApp, accountNumber, sequence, nextValidatorsHash := setupTreasuryMultisigApp(
				t,
				committeePubKey,
				!test.omitCommitteeAccount,
				committeeFeeBalance,
				func(_ *treasurytypes.GenesisState, genesis *claimstypes.GenesisState, committee string) {
					genesis.ClaimsMandate = claimstypes.ClaimsMandate{
						Envelope: mandate.Envelope{
							Term:             1,
							Committee:        committee,
							ActivationHeight: 1,
							ExpiryHeight:     1_000_000,
						},
						CommitteeClaimLimit: sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
					}
				},
			)
			sequence += test.sequenceOffset
			const claimID uint64 = 1
			msg := &claimstypes.MsgCommitteeSubmitClaim{
				Committee:    committee.String(),
				ExpectedTerm: 1,
				Reference:    "incident-1",
				Recipient:    sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
				Amount:       sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
			}

			txBytes := buildTreasuryMultisigTx(
				t,
				arkApp,
				msg,
				committeePubKey,
				accountNumber,
				sequence,
				test.signatures,
			)
			response, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
				Height:             1,
				Hash:               arkApp.LastCommitID().Hash,
				NextValidatorsHash: nextValidatorsHash,
				Txs:                [][]byte{txBytes},
			})
			require.NoError(t, err)
			require.Len(t, response.TxResults, 1)
			if test.wantClaim {
				require.Zero(t, response.TxResults[0].Code, response.TxResults[0].Log)
			} else {
				require.NotZero(t, response.TxResults[0].Code)
				if test.wantLogContains != "" {
					require.Contains(t, response.TxResults[0].Log, test.wantLogContains)
				}
			}
			_, err = arkApp.Commit()
			require.NoError(t, err)

			ctx := arkApp.NewContextLegacy(true, cmtproto.Header{
				ChainID: treasuryMultisigChainID,
				Height:  1,
			})
			hasClaim, err := arkApp.ClaimsKeeper.Claims.Has(ctx, claimID)
			require.NoError(t, err)
			require.Equal(t, test.wantClaim, hasClaim)
			nextClaimID, err := arkApp.ClaimsKeeper.NextClaimID.Peek(ctx)
			require.NoError(t, err)
			if !test.wantClaim {
				require.Equal(t, uint64(1), nextClaimID)
				return
			}
			require.Equal(t, uint64(2), nextClaimID)

			claim, err := arkApp.ClaimsKeeper.Claims.Get(ctx, claimID)
			require.NoError(t, err)
			require.Equal(t, committee.String(), claim.Submitter)
			require.Equal(
				t,
				claimstypes.ClaimStatus_CLAIM_STATUS_PENDING,
				claim.Status,
			)
		})
	}
}

// The threshold matrix is the claims test's above. What only this adds is that
// MsgCommitteeUpdatePolicy carries an amino name a legacy multisig can sign
// against, and that a policy inside the mandate's corridor lands.
func TestTreasuryEconomicPolicyLegacyAminoMultisig(t *testing.T) {
	members := []cryptotypes.PrivKey{
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
	}
	memberPubKeys := make([]cryptotypes.PubKey, len(members))
	for i, member := range members {
		memberPubKeys[i] = member.PubKey()
	}
	committeePubKey := kmultisig.NewLegacyAminoPubKey(2, memberPubKeys)
	committee := sdk.AccAddress(committeePubKey.Address())

	arkApp, accountNumber, sequence, nextValidatorsHash := setupTreasuryMultisigApp(
		t,
		committeePubKey,
		true,
		multisigCommitteeFeeBalance,
		func(genesis *treasurytypes.GenesisState, _ *claimstypes.GenesisState, committee string) {
			minimum := treasurytypes.DefaultEconomicPolicy()
			maximum := treasurytypes.EconomicPolicy{
				ValidatorBlockRewardTarget:  math.NewInt(10),
				OracleBlockRewardTarget:     math.NewInt(10),
				RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
				StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
				InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.5"),
				LiabilityRatioWeight:        math.LegacyOneDec(),
				VolatilityWeight:            math.LegacyOneDec(),
				FlowWeight:                  math.LegacyOneDec(),
			}
			genesis.EconomicMandate = treasurytypes.EconomicMandate{
				Envelope: mandate.Envelope{
					Term:             1,
					Committee:        committee,
					ActivationHeight: 1,
					ExpiryHeight:     100,
				},
				MinimumPolicy: minimum,
				MaximumPolicy: maximum,
			}
		},
	)
	policy := treasurytypes.EconomicPolicy{
		ValidatorBlockRewardTarget:  math.NewInt(5),
		OracleBlockRewardTarget:     math.NewInt(5),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.25"),
		LiabilityRatioWeight:        math.LegacyMustNewDecFromStr("0.5"),
		VolatilityWeight:            math.LegacyZeroDec(),
		FlowWeight:                  math.LegacyZeroDec(),
	}
	msg := &treasurytypes.MsgCommitteeUpdatePolicy{
		Committee:    committee.String(),
		ExpectedTerm: 1,
		Policy:       policy,
	}
	txBytes := buildTreasuryMultisigTx(
		t,
		arkApp,
		msg,
		committeePubKey,
		accountNumber,
		sequence,
		[]treasuryMultisigMemberSignature{
			{memberIndex: 0, privateKey: members[0]},
			{memberIndex: 2, privateKey: members[2]},
		},
	)
	response, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             1,
		Hash:               arkApp.LastCommitID().Hash,
		NextValidatorsHash: nextValidatorsHash,
		Txs:                [][]byte{txBytes},
	})
	require.NoError(t, err)
	require.Len(t, response.TxResults, 1)
	require.Zero(t, response.TxResults[0].Code, response.TxResults[0].Log)
	_, err = arkApp.Commit()
	require.NoError(t, err)

	ctx := arkApp.NewContextLegacy(true, cmtproto.Header{ChainID: treasuryMultisigChainID, Height: 1})
	storedPolicy, err := arkApp.TreasuryKeeper.EconomicPolicy.Get(ctx)
	require.NoError(t, err)
	require.True(t, policy.Equal(storedPolicy))
}

func setupTreasuryMultisigApp(
	t *testing.T,
	committeePubKey *kmultisig.LegacyAminoPubKey,
	includeCommitteeAccount bool,
	committeeFeeBalance int64,
	configureGenesis func(*treasurytypes.GenesisState, *claimstypes.GenesisState, string),
) (*app.ArkApp, uint64, uint64, []byte) {
	t.Helper()

	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(treasuryMultisigChainID),
	)
	committee := sdk.AccAddress(committeePubKey.Address())
	validatorKey := secp256k1.GenPrivKey()
	validatorAddress := sdk.AccAddress(validatorKey.PubKey().Address())
	validatorAccount := authtypes.NewBaseAccount(validatorAddress, validatorKey.PubKey(), 0, 0)
	genesisAccounts := []authtypes.GenesisAccount{validatorAccount}
	balances := []banktypes.Balance{
		{
			Address: validatorAddress.String(),
			Coins: sdk.NewCoins(
				sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction.MulRaw(100_000)),
			),
		},
		{
			Address: authtypes.NewModuleAddress(claimstypes.InsuranceName).String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000)),
		},
	}
	if includeCommitteeAccount {
		committeeAccount := authtypes.NewBaseAccount(committee, committeePubKey, 1, 0)
		genesisAccounts = append(genesisAccounts, committeeAccount)
		balances = append(balances, banktypes.Balance{
			Address: committee.String(),
			Coins: sdk.NewCoins(
				sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(10_000_000_000)),
				// The fee gate prices gas in the reference denom, so fees are
				// paid — and the starved case funded short — in axdr.
				sdk.NewCoin(chain.XDRBaseDenom, math.NewInt(committeeFeeBalance)),
			),
		})
	}
	validatorSet, err := simtestutil.CreateRandomValidatorSet()
	require.NoError(t, err)

	genesisState := arkApp.DefaultGenesis()
	genesisState, err = simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		genesisState,
		validatorSet,
		genesisAccounts,
		balances...,
	)
	require.NoError(t, err)

	treasuryGenesis := treasurytypes.DefaultGenesisState()
	claimsGenesis := claimstypes.DefaultGenesisState()
	configureGenesis(treasuryGenesis, claimsGenesis, committee.String())
	genesisState[treasurytypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(treasuryGenesis)
	genesisState[claimstypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(claimsGenesis)

	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		ChainId:         treasuryMultisigChainID,
		Validators:      []cmtabci.ValidatorUpdate{},
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{ChainID: treasuryMultisigChainID})
	storedAccount := arkApp.AccountKeeper.GetAccount(ctx, committee)
	if !includeCommitteeAccount {
		require.Nil(t, storedAccount)
		return arkApp, 0, 0, validatorSet.Hash()
	}
	require.NotNil(t, storedAccount)
	require.NotNil(t, storedAccount.GetPubKey())
	require.True(t, committeePubKey.Equals(storedAccount.GetPubKey()))
	return arkApp, storedAccount.GetAccountNumber(), storedAccount.GetSequence(), validatorSet.Hash()
}

func buildTreasuryMultisigTx(
	t *testing.T,
	arkApp *app.ArkApp,
	msg sdk.Msg,
	committeePubKey *kmultisig.LegacyAminoPubKey,
	accountNumber,
	sequence uint64,
	members []treasuryMultisigMemberSignature,
) []byte {
	t.Helper()

	txBuilder := arkApp.GetTxConfig().NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, multisigFeeAmount)))
	txBuilder.SetGasLimit(multisigGasLimit)

	emptyMultisignature := cryptomultisig.NewMultisig(len(committeePubKey.GetPubKeys()))
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   committeePubKey,
		Data:     emptyMultisignature,
		Sequence: sequence,
	}))
	signerData := authsigning.SignerData{
		Address:       sdk.AccAddress(committeePubKey.Address()).String(),
		ChainID:       treasuryMultisigChainID,
		AccountNumber: accountNumber,
		Sequence:      sequence,
		PubKey:        committeePubKey,
	}
	signBytes, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		arkApp.GetTxConfig().SignModeHandler(),
		signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
		signerData,
		txBuilder.GetTx(),
	)
	require.NoError(t, err)

	memberPubKeys := committeePubKey.GetPubKeys()
	multisignature := cryptomultisig.NewMultisig(len(memberPubKeys))
	for _, member := range members {
		require.Less(t, member.memberIndex, len(memberPubKeys))
		signatureBytes, err := member.privateKey.Sign(signBytes)
		require.NoError(t, err)
		memberSignature := signing.SignatureV2{
			PubKey: memberPubKeys[member.memberIndex],
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
				Signature: signatureBytes,
			},
			Sequence: sequence,
		}
		require.NoError(t, cryptomultisig.AddSignatureV2(multisignature, memberSignature, memberPubKeys))
	}
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   committeePubKey,
		Data:     multisignature,
		Sequence: sequence,
	}))

	txBytes, err := arkApp.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// TestClaimsCommitteeShapeRecordsRegisteredMultisig proves the appointment
// path reaches a real account: a genuine 2-of-3 whose key is registered on
// chain is recorded as one, rather than as the keyless shape an address with
// no registered key would produce.
func TestClaimsCommitteeShapeRecordsRegisteredMultisig(t *testing.T) {
	memberPubKeys := make([]cryptotypes.PubKey, 3)
	for i := range memberPubKeys {
		memberPubKeys[i] = secp256k1.GenPrivKey().PubKey()
	}
	committeePubKey := kmultisig.NewLegacyAminoPubKey(2, memberPubKeys)
	committee := sdk.AccAddress(committeePubKey.Address())

	arkApp, _, _, _ := setupTreasuryMultisigApp(
		t,
		committeePubKey,
		true,
		multisigCommitteeFeeBalance,
		func(_ *treasurytypes.GenesisState, genesis *claimstypes.GenesisState, _ string) {
			genesis.ClaimsMandate = claimstypes.DefaultClaimsMandate()
		},
	)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		ChainID: treasuryMultisigChainID,
		Height:  1,
	})

	_, err := claimskeeper.NewMsgServerImpl(arkApp.ClaimsKeeper).SetClaimsMandate(
		ctx,
		&claimstypes.MsgSetClaimsMandate{
			Authority:           authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			Committee:           committee.String(),
			ActivationHeight:    1,
			ExpiryHeight:        chain.BlocksPerYear,
			CommitteeClaimLimit: chain.NoahCoin(math.NewInt(1_000)),
		},
	)
	require.NoError(t, err)

	stored, err := arkApp.ClaimsKeeper.ClaimsMandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, mandate.CommitteeShape{
		AccountType: "cosmos.auth.v1beta1.BaseAccount",
		KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
		Threshold:   2,
		MemberCount: 3,
	}, stored.CommitteeShape)
}
