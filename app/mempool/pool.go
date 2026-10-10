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

	mempoolmetrics "github.com/ararat-network/ark/app/mempool/metrics"
)

var ErrCapacity = errorsmod.Register("lanes", 1, "lane admission capacity exhausted")

// rebuildDepartures floors the departed senders a rebuild waits for, so a small
// pool does not rebuild on every departure.
const rebuildDepartures = 4096

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
	seen     uint64      // epoch of the last recheck that held it, or of insertion
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
	epoch     uint64         // one per commit, bumped before CometBFT's recheck round
	senders   map[string]int // pending entries per first signer
	departed  int            // senders whose last pending entry left since the index was built
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
		entries: make(map[senderNonce]Entry), hashes: make(map[[32]byte]senderNonce), senders: make(map[string]int),
	}
	// Normal storage takes the remainder, including integer rounding.
	p.countCaps[LaneGovernance] = int(fraction(uint64(config.MaxTxs), GovernanceAdmissionCountShare))
	p.countCaps[LaneCommittee] = int(fraction(uint64(config.MaxTxs), CommitteeAdmissionCountShare))
	p.countCaps[LaneNormal] = config.MaxTxs - p.countCaps[LaneGovernance] - p.countCaps[LaneCommittee]
	p.byteCaps = config.byteCaps()
	mempoolmetrics.ObservePool(p.Usage)
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
	} else {
		p.senders[key.sender]++
	}
	entry := Entry{Tx: tx, Key: sha256.Sum256(ctx.TxBytes()), Size: len(ctx.TxBytes()), Lane: lane, Slot: slot, Priority: ctx.Priority(), key: key, seen: p.epoch}
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
	if p.senders[key.sender]--; p.senders[key.sender] == 0 {
		delete(p.senders, key.sender)
		p.departed++
	}
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

// touch records that CometBFT's recheck still holds these wire bytes.
func (p *Pool) touch(bz []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key, ok := p.hashes[sha256.Sum256(bz)]; ok {
		e := p.entries[key]
		e.seen = p.epoch
		p.entries[key] = e
	}
}

// PrepareCheckState releases committed entries, then drops those CometBFT's list
// no longer holds. The recheck after every commit touches each listed entry, so
// an entry two rounds passed over was refused by the list after admission and no
// recheck will ever release it; the second round is slack for a timed-out recheck.
// CometBFT and SDK CheckTx own the recheck itself; there is no application
// revalidation loop.
func (p *Pool) PrepareCheckState(sdk.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, bz := range p.finalised {
		if key, ok := p.hashes[sha256.Sum256(bz)]; ok {
			_ = p.remove(key)
		}
	}
	p.finalised = nil
	p.epoch++
	// A listed entry lags one epoch; three means two rounds passed it over.
	for key, e := range p.entries {
		if p.epoch-e.seen > 2 {
			_ = p.remove(key)
		}
	}
	if p.departed > 0 && (len(p.entries) == 0 || p.departed >= max(len(p.entries), rebuildDepartures)) {
		p.rebuild()
	}
}

// rebuild replaces the SDK index, which keeps a skiplist and its random source
// (about 6 KB) for every sender it has ever held; upstream Remove never frees an
// empty one. Waiting until departed senders outnumber live entries bounds that
// garbage by the live index; an empty pool rebuilds for free. Entries re-enter
// with their stored lane and priority, as the proposal index does.
func (p *Pool) rebuild() {
	cfg := priorityConfig()
	cfg.MaxTx = p.config.MaxTxs
	index := sdkmempool.NewPriorityMempool(cfg)
	base := sdk.Context{}.WithContext(context.Background())
	for _, e := range p.entries {
		// Every entry passed this insertion once, so a failure is unreachable; keep the old index.
		if err := index.Insert(WithLane(base.WithPriority(e.Priority), e.Lane), e.Tx); err != nil {
			return
		}
	}
	p.index = index
	p.departed = 0
}

// live reads the SDK index pointer, which rebuild replaces under mu.
func (p *Pool) live() *sdkmempool.PriorityNonceMempool[priority] {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.index
}

func (p *Pool) CountTx() int { return p.live().CountTx() }

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
	return p.live().Select(ctx, txs)
}

func (p *Pool) SelectBy(ctx context.Context, txs [][]byte, visit func(sdk.Tx) bool) {
	p.live().SelectBy(ctx, txs, visit)
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
