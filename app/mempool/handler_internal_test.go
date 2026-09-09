package mempool

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type lifecycleApp struct {
	Application
	finalizeErr error
	commitErr   error
}

func (a *lifecycleApp) FinalizeBlock(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return &abci.ResponseFinalizeBlock{}, a.finalizeErr
}

func (a *lifecycleApp) Commit() (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, a.commitErr
}

func TestLifecycleRetainsPendingUntilCommit(t *testing.T) {
	for _, failure := range []string{"finalise", "commit"} {
		t.Run(failure, func(t *testing.T) {
			pool := newTestPool(20, encodePoolTx)
			tx := fixtureTx(1)
			require.NoError(t, pool.Insert(laneContext(LaneNormal), tx))
			bz, err := encodePoolTx(tx)
			require.NoError(t, err)
			app := &lifecycleApp{}
			h := NewHandler(app, pool, nil, Set{})

			_, err = h.FinalizeBlock(&abci.RequestFinalizeBlock{Txs: [][]byte{bz}})
			require.NoError(t, err)
			require.True(t, pool.Has(bz), "finalisation alone cannot retire pending transactions")

			failureErr := errors.New("store transition failed")
			switch failure {
			case "finalise":
				app.finalizeErr = failureErr
				// A failed candidate must not replace the last successfully
				// finalised transaction set awaiting commitment.
				_, err = h.FinalizeBlock(&abci.RequestFinalizeBlock{})
			case "commit":
				app.commitErr = failureErr
				_, err = h.Commit()
			}
			require.ErrorIs(t, err, failureErr)
			require.True(t, pool.Has(bz), "failed store transitions must retain pending transactions")

			app.commitErr = nil
			_, err = h.Commit()
			require.NoError(t, err)
			require.Zero(t, pool.CountTx(), "only successful commitment retires the included transaction")
		})
	}
}

func TestConfiguredSizeCheckedBeforeDecode(t *testing.T) {
	for _, limit := range []int{512, 2 << 20} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxBytes = limit
			calls := 0
			h := NewHandler(nil, NewPool(cfg, encodePoolTx), func([]byte) (sdk.Tx, error) {
				calls++
				return nil, errors.New("decoder reached")
			}, Set{})
			response, err := h.CheckTx(&abci.RequestCheckTx{Tx: make([]byte, limit+1)})
			require.NoError(t, err)
			require.Contains(t, response.Log, "invalid transaction size")
			require.Zero(t, calls)
			response, err = h.CheckTx(&abci.RequestCheckTx{Tx: make([]byte, limit)})
			require.NoError(t, err)
			require.Contains(t, response.Log, "decoder reached")
			require.Equal(t, 1, calls)
		})
	}
}

type inspectionTx struct {
	*poolTx
	panicAt string
}

func (tx *inspectionTx) GetMsgs() []sdk.Msg {
	if tx.panicAt == "candidate" {
		panic("candidate inspection failed")
	}
	return tx.poolTx.GetMsgs()
}

func (tx *inspectionTx) GetGas() uint64 {
	if tx.panicAt == "gas" {
		panic("gas inspection failed")
	}
	return tx.poolTx.GetGas()
}

type inspectionApp struct {
	Application
	ctx sdk.Context
}

func (a *inspectionApp) GetContextForCheckTx([]byte) sdk.Context { return a.ctx }

func TestAdmissionRecoversInspectionPanics(t *testing.T) {
	for _, path := range []string{"rpc", "peer"} {
		for _, stage := range []string{"decoder", "candidate", "gas"} {
			t.Run(path+"/"+stage, func(t *testing.T) {
				pool := newTestPool(20, encodePoolTx)
				require.NoError(t, pool.Insert(laneContext(LaneNormal), fixtureTx(2)))
				before := pool.Snapshot()
				counts, sizes := pool.Usage()
				tx := &inspectionTx{poolTx: fixtureTx(1), panicAt: stage}
				app := &inspectionApp{ctx: sdk.Context{}.WithConsensusParams(cmtproto.ConsensusParams{
					Block: &cmtproto.BlockParams{MaxGas: 10},
				})}
				h := NewHandler(app, pool, func([]byte) (sdk.Tx, error) {
					if tx.panicAt == "decoder" {
						panic("decoder inspection failed")
					}
					return tx, nil
				}, Set{})
				check := func(wantCode uint32) {
					t.Helper()
					if path == "peer" {
						res, err := h.InsertTx(&abci.RequestInsertTx{Tx: []byte{1}})
						require.NoError(t, err)
						require.Equal(t, wantCode, res.Code)
					} else {
						res, err := h.CheckTx(&abci.RequestCheckTx{Tx: []byte{1}})
						require.NoError(t, err)
						require.Equal(t, wantCode, res.Code, res.Log)
						if wantCode == sdkerrors.ErrPanic.ABCICode() {
							require.Equal(t, sdkerrors.ErrPanic.Codespace(), res.Codespace)
							require.Contains(t, res.Log, stage+" inspection failed")
						} else {
							require.Equal(t, sdkerrors.ErrOutOfGas.Codespace(), res.Codespace)
						}
					}
				}
				// More failures than queue capacity must still reach inspection.
				for range cap(h.decodeSlots) + 1 {
					check(sdkerrors.ErrPanic.ABCICode())
					require.Empty(t, h.decodeSlots)
					for _, slots := range h.admissionSlots {
						require.Empty(t, slots)
					}
					require.True(t, h.admissionMu.TryLock(), "inspection failure must release the admission lock")
					h.admissionMu.Unlock()
				}
				// An ordinary later request reaches the block gas check rather than
				// being refused by a leaked slot or stuck behind the admission lock.
				tx.panicAt = ""
				check(sdkerrors.ErrOutOfGas.ABCICode())
				require.Equal(t, before, pool.Snapshot())
				afterCounts, afterSizes := pool.Usage()
				require.Equal(t, counts, afterCounts)
				require.Equal(t, sizes, afterSizes)
			})
		}
	}
}
