package mempool

import (
	"cmp"
	"context"
	"crypto/sha256"
	"math"
	"sync"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
)

var ErrCapacity = errorsmod.Register("lanes", 3, "lane admission capacity exhausted")

// Entry adds reservation metadata to a transaction held by the SDK index.
// Size is the admitted wire length; proposals re-encode through the SDK.
type Entry struct {
	Tx       sdk.Tx
	Key      [32]byte
	Size     int
	Lane     int8
	Slot     int8
	Priority int64
	key      senderNonce // SDK index identity, so proposals need not re-extract signers
}

type senderNonce struct {
	sender string
	nonce  uint64
}

// priority keeps lanes disjoint without compressing the SDK's int64 fee priority.
// The SDK still owns nonce ordering and priority tie-breaking.
type priority struct {
	lane int8
	fee  int64
}

func priorityConfig() sdkmempool.PriorityNonceMempoolConfig[priority] {
	return sdkmempool.PriorityNonceMempoolConfig[priority]{
		TxPriority: sdkmempool.TxPriority[priority]{
			GetTxPriority: func(goCtx context.Context, _ sdk.Tx) priority {
				ctx := sdk.UnwrapSDKContext(goCtx)
				return priority{lane: Lane(ctx), fee: ctx.Priority()}
			},
			Compare: func(a, b priority) int {
				if order := cmp.Compare(a.lane, b.lane); order != 0 {
					return order
				}
				return cmp.Compare(a.fee, b.fee)
			},
			MinValue: priority{lane: LaneNormal, fee: math.MinInt64},
		},
	}
}

// Pool extends the SDK priority/nonce pool with count and byte reservations.
// CometBFT's local client serialises ante admission with insertion. The mutex
// protects metadata and SDK index updates; no ante handler runs under it.
type Pool struct {
	mu        sync.Mutex
	index     *sdkmempool.PriorityNonceMempool[priority]
	entries   map[senderNonce]Entry
	hashes    map[[32]byte]senderNonce
	counts    [3]int
	sizes     [3]int64
	countCaps [3]int
	byteCaps  [3]int64
	config    Config
	finalised [][]byte
}

var _ sdkmempool.ExtMempool = (*Pool)(nil)

func NewPool(config Config) *Pool {
	if err := config.Validate(); err != nil {
		panic(err)
	}
	config.MaxTxs = config.Count()
	cfg := priorityConfig()
	cfg.MaxTx = config.MaxTxs
	p := &Pool{
		index: sdkmempool.NewPriorityMempool(cfg), config: config,
		entries: make(map[senderNonce]Entry), hashes: make(map[[32]byte]senderNonce),
	}
	// Normal storage takes the remainder, including integer rounding.
	p.countCaps[LaneGovernance] = int(fraction(uint64(config.MaxTxs), GovernanceAdmissionCountShare))
	p.countCaps[LaneCommittee] = int(fraction(uint64(config.MaxTxs), CommitteeAdmissionCountShare))
	p.countCaps[LaneNormal] = config.MaxTxs - p.countCaps[LaneGovernance] - p.countCaps[LaneCommittee]
	p.byteCaps[LaneGovernance] = int64(fraction(uint64(config.MaxTxsBytes), GovernanceAdmissionByteShare))
	p.byteCaps[LaneCommittee] = int64(fraction(uint64(config.MaxTxsBytes), CommitteeAdmissionByteShare))
	p.byteCaps[LaneNormal] = config.MaxTxsBytes - p.byteCaps[LaneGovernance] - p.byteCaps[LaneCommittee]
	abcimetrics.ObservePool(p.Usage)
	return p
}

// identity uses the same first signer and nonce as the SDK's default index.
func identity(tx sdk.Tx) (senderNonce, error) {
	signers, err := sdkmempool.NewDefaultSignerExtractionAdapter().GetSigners(tx)
	if err != nil {
		return senderNonce{}, err
	}
	if len(signers) == 0 {
		return senderNonce{}, sdkerrors.ErrNoSignatures
	}
	nonce, err := sdkmempool.ChooseNonce(signers[0].Sequence, tx)
	return senderNonce{sender: string(signers[0].Signer), nonce: nonce}, err
}

// allocation is read-only so a refusal can happen inside SDK ante's cache,
// before RunTx retains fee or sequence writes. The caller holds mu.
func (p *Pool) allocation(key senderNonce, lane int8, size int) (int8, error) {
	if size > p.config.MaxTxBytes {
		return 0, sdkerrors.ErrTxTooLarge
	}
	// Match the SDK's capacity-before-replacement rule.
	if len(p.entries) >= p.config.MaxTxs {
		return 0, sdkmempool.ErrMempoolTxMaxCapacity
	}
	counts, sizes := p.counts, p.sizes
	if old, ok := p.entries[key]; ok {
		counts[old.Slot]--
		sizes[old.Slot] -= int64(old.Size)
	}
	fits := func(slot int8) bool {
		return counts[slot] < p.countCaps[slot] && int64(size) <= p.byteCaps[slot]-sizes[slot]
	}
	if fits(lane) {
		return lane, nil
	}
	if lane != LaneNormal && fits(LaneNormal) {
		return LaneNormal, nil
	}
	return 0, ErrCapacity
}

func (p *Pool) Insert(goCtx context.Context, tx sdk.Tx) error {
	ctx := sdk.UnwrapSDKContext(goCtx)
	key, err := identity(tx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	lane := Lane(ctx)
	slot, err := p.allocation(key, lane, len(ctx.TxBytes()))
	if err != nil {
		return err
	}
	if err := p.index.Insert(goCtx, tx); err != nil {
		return err
	}
	if old, ok := p.entries[key]; ok {
		p.counts[old.Slot]--
		p.sizes[old.Slot] -= int64(old.Size)
		delete(p.hashes, old.Key)
	}
	entry := Entry{Tx: tx, Key: sha256.Sum256(ctx.TxBytes()), Size: len(ctx.TxBytes()), Lane: lane, Slot: slot, Priority: ctx.Priority(), key: key}
	p.entries[key] = entry
	p.hashes[entry.Key] = key
	p.counts[slot]++
	p.sizes[slot] += int64(entry.Size)
	return nil
}

func (p *Pool) Remove(tx sdk.Tx) error {
	key, err := identity(tx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.remove(key)
}

func (p *Pool) remove(key senderNonce) error {
	entry, ok := p.entries[key]
	if !ok {
		return sdkmempool.ErrTxNotFound
	}
	if err := p.index.Remove(entry.Tx); err != nil {
		return err
	}
	delete(p.entries, key)
	delete(p.hashes, entry.Key)
	p.counts[entry.Slot]--
	p.sizes[entry.Slot] -= int64(entry.Size)
	return nil
}

// RemoveWithReason releases rechecked transactions while CometBFT updates its
// list. Earlier removals (including
// optimistic execution and proposal filtering) must not free reserved headroom:
// the flood list still holds those bytes and could otherwise fill with normals.
func (p *Pool) RemoveWithReason(_ context.Context, tx sdk.Tx, reason sdkmempool.RemoveReason) error {
	if reason.Caller != sdkmempool.CallerRunTxRecheck {
		return nil
	}
	return p.Remove(tx)
}

func (p *Pool) Finalised(txs [][]byte) { p.mu.Lock(); defer p.mu.Unlock(); p.finalised = txs }

// PrepareCheckState only releases committed entries. CometBFT and SDK CheckTx
// own the subsequent recheck; there is no application revalidation loop.
func (p *Pool) PrepareCheckState(sdk.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, bz := range p.finalised {
		if key, ok := p.hashes[sha256.Sum256(bz)]; ok {
			_ = p.remove(key)
		}
	}
	p.finalised = nil
}

func (p *Pool) CountTx() int { return p.index.CountTx() }

// Usage reports occupancy by allocation; the pool gauges read it at scrape time.
func (p *Pool) Usage() ([3]int, [3]int64) { p.mu.Lock(); defer p.mu.Unlock(); return p.counts, p.sizes }

// Has reports whether the admitted wire bytes are still pending.
func (p *Pool) Has(bz []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.hashes[sha256.Sum256(bz)]
	return ok
}

// Select and SelectBy retain the SDK's iterator semantics. As in the SDK,
// Select is not safe for concurrent iteration; SelectBy holds the index lock.
func (p *Pool) Select(ctx context.Context, txs [][]byte) sdkmempool.Iterator {
	return p.index.Select(ctx, txs)
}

func (p *Pool) SelectBy(ctx context.Context, txs [][]byte, visit func(sdk.Tx) bool) {
	p.index.SelectBy(ctx, txs, visit)
}

// Snapshot copies metadata in SDK priority/nonce order. Callers validate only
// after this method returns, outside both locks.
func (p *Pool) Snapshot() []Entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	entries := make([]Entry, 0, len(p.entries))
	p.index.SelectBy(context.Background(), nil, func(tx sdk.Tx) bool {
		key, err := identity(tx)
		if err != nil {
			panic(err)
		} // Every indexed transaction passed SDK insertion.
		entries = append(entries, p.entries[key])
		return true
	})
	return entries
}
