package mempool

import (
	"fmt"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
)

// Application is the SDK state and lifecycle surface used by Handler. Supply
// the embedded SDK application, not the outer app that forwards calls here.
type Application interface {
	GetContextForCheckTx([]byte) sdk.Context
	RunTx(sdk.ExecMode, []byte, sdk.Tx, int, storetypes.MultiStore, map[string]any) (sdk.GasInfo, *sdk.Result, []abci.Event, error)
	MsgServiceRouter() *baseapp.MsgServiceRouter
	AnteHandler() sdk.AnteHandler
	InitChain(*abci.RequestInitChain) (*abci.ResponseInitChain, error)
	FinalizeBlock(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error)
	Commit() (*abci.ResponseCommit, error)
	PrepareProposal(*abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error)
	ProcessProposal(*abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error)
	ApplySnapshotChunk(*abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error)
	Close() error
}

// Handler owns admission, proposal assembly, and the pending pool's lifecycle.
// Its admission mutex fences SDK check-state writes and root-store transitions;
// Pool separately protects its transaction index without holding its lock in ante.
type Handler struct {
	app        Application
	pool       *Pool
	decode     sdk.TxDecoder
	privileges Set
	post       sdk.PostHandler

	admissionMu    sync.Mutex
	admissionSlots [3]chan struct{}
	decodeSlots    chan struct{}
	finalizedTxs   [][]byte
}

// NewHandler connects the pool installed in BaseApp to its SDK application.
// The privilege set must be shared with the app's ante wiring. AnteHandler is
// read from the application when validating a transaction.
func NewHandler(app Application, pool *Pool, decode sdk.TxDecoder, privileges Set) *Handler {
	h := &Handler{
		app: app, pool: pool, decode: decode, privileges: privileges,
		decodeSlots: make(chan struct{}, 8),
	}
	for i := range h.admissionSlots {
		h.admissionSlots[i] = make(chan struct{}, 8)
	}
	abcimetrics.RecordPool(pool.Usage())
	return h
}

// SetPostHandler installs the post chain that recheck and proposal validation
// run after ante, as RunTx does. BaseApp has no getter, so the app installs
// the same chain on both.
func (h *Handler) SetPostHandler(post sdk.PostHandler) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	h.post = post
}

// Privileges returns the compiled registry shared with authenticated ante checks.
func (h *Handler) Privileges() Set { return h.privileges }

func (h *Handler) ReapTxs(req *abci.RequestReapTxs) (*abci.ResponseReapTxs, error) {
	if req == nil {
		return nil, fmt.Errorf("nil ReapTxs")
	}
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	return &abci.ResponseReapTxs{Txs: h.pool.Gossip(req.MaxBytes, req.MaxGas, time.Now())}, nil
}

// InitChain and the other lifecycle methods fence admission against writes to the root store and
// replacement of CheckTx state. Optimistic execution writes its private branch;
// Pool.RemoveWithReason waits for actual commitment before retiring entries.
func (h *Handler) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	return h.app.InitChain(req)
}

func (h *Handler) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	res, err := h.app.FinalizeBlock(req)
	if err == nil {
		h.finalizedTxs = req.Txs
	}
	return res, err
}

func (h *Handler) Commit() (*abci.ResponseCommit, error) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	res, err := h.app.Commit()
	if err != nil {
		return res, err
	}
	for _, bz := range h.finalizedTxs {
		h.pool.RemoveBytes(bz)
	}
	h.finalizedTxs = nil
	pending := h.pool.Snapshot()
	h.pool.Reset()
	// Arrival order preserves accepted cross-signer fee and nonce dependencies.
	// Keep identity and gossip history when refreshing eligibility and storage.
	// Each failed recheck discards its state; successors then fail naturally.
	for _, e := range pending {
		h.recheck(e)
	}
	abcimetrics.RecordPool(h.pool.Usage())
	return res, nil
}

func (h *Handler) PrepareProposal(req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	return h.app.PrepareProposal(req)
}

func (h *Handler) ProcessProposal(req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	return h.app.ProcessProposal(req)
}

func (h *Handler) ApplySnapshotChunk(req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	h.pool.Reset()
	abcimetrics.RecordPool(h.pool.Usage())
	return h.app.ApplySnapshotChunk(req)
}

// Close fences store shutdown against admission and lifecycle work.
// The application owns idempotence of its overall shutdown.
func (h *Handler) Close() error {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	return h.app.Close()
}
