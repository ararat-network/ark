package mempool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
)

var (
	ErrCapacity  = errors.New("lane admission capacity exhausted")
	ErrDuplicate = errors.New("transaction already admitted")
)

// The SDK's RemoveWithReason helper falls back to Remove when this extension
// interface is unsatisfied, which would retire pending entries on optimistic
// finalisation. Upstream main changes Insert and SelectBy; re-check on a bump.
var _ sdkmempool.ExtMempool = (*Pool)(nil)

// Entry is an immutable snapshot of an admitted transaction. Bytes must not be mutated.
type Entry struct {
	Tx         sdk.Tx
	Bytes      []byte
	Key        [32]byte
	Lane       int8 // Authenticated scheduling eligibility.
	Fee        int64
	Signers    []sdkmempool.SignerData
	Unordered  bool
	Nonce      uint64
	Added      uint64
	Slot       int8 // Admission storage partition charged for this entry.
	LastGossip time.Time
}

// Pool owns the pending transactions for both proposal selection and gossip.
// Snapshots feed the shared nonce scheduler; admission indexes only track storage.
// Admission is serialised with check-state writes by the app, independently of
// this mutex. No caller holds this mutex while executing ante or a callback.
type Pool struct {
	mu      sync.Mutex
	encode  sdk.TxEncoder
	entries map[[32]byte]Entry
	nonces  map[senderNonce]struct{}
	senders map[laneSender]senderUsage
	votes   map[string]int
	counts  [3]int
	bytes   [3]int64
	config  Config
	serial  uint64
}

type senderNonce struct {
	sender string
	nonce  uint64
}

type laneSender struct {
	lane   int8
	sender string
}

type senderUsage struct {
	count int
	bytes int64
}

func NewPool(config Config, encode sdk.TxEncoder) *Pool {
	if err := config.Validate(); err != nil {
		panic(err)
	}
	if config.MaxTxs == 0 {
		config.MaxTxs = DefaultMaxTx
	}
	p := &Pool{encode: encode, config: config}
	p.Reset()
	return p
}

// Reset clears pending storage while preserving the arrival counter so new
// transactions sort after surviving entries reinserted during revalidation.
func (p *Pool) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make(map[[32]byte]Entry)
	p.nonces = make(map[senderNonce]struct{})
	p.senders = make(map[laneSender]senderUsage)
	p.votes = make(map[string]int)
	p.counts = [3]int{}
	p.bytes = [3]int64{}
}

func (p *Pool) Has(bz []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.entries[sha256.Sum256(bz)]
	return ok
}

func (p *Pool) CountTx() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

func (p *Pool) Usage() ([3]int, [3]int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts, p.bytes
}

// Snapshot returns shallow entry copies in arrival order. Callers must treat
// the shared transaction objects, byte slices and signer slices as immutable.
func (p *Pool) Snapshot() []Entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Entry, 0, len(p.entries))
	for _, e := range p.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Added < out[j].Added })
	return out
}

// MayFit is only an inexpensive rejection filter. A type hint cannot authorise
// insertion; Insert uses the authenticated lane after all transaction checks.
func (p *Pool) MayFit(lane int8, size int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fits(lane, size) || p.fits(LaneNormal, size)
}

func (p *Pool) Insert(ctx context.Context, tx sdk.Tx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// SDK RunTx carries the original wire bytes; their hash is the pool identity.
	// Direct pool callers without transaction context still use the encoder.
	bz := sdkCtx.TxBytes()
	if len(bz) == 0 {
		var err error
		bz, err = p.encode(tx)
		if err != nil {
			return err
		}
	}
	e, err := p.newEntry(tx, bz)
	if err != nil {
		return err
	}
	e.Lane = FromContext(sdkCtx)
	e.Fee = sdkCtx.Priority()

	return p.insert(e)
}

// newEntry captures transaction identity once, after successful ante validation.
// Rechecks reuse this snapshot and refresh only the authenticated lane and fee.
func (p *Pool) newEntry(tx sdk.Tx, bz []byte) (Entry, error) {
	if len(bz) > p.config.MaxTxBytes {
		return Entry{}, fmt.Errorf("transaction exceeds %d bytes", p.config.MaxTxBytes)
	}
	signers, err := extractSigners(tx)
	if err != nil {
		return Entry{}, err
	}
	unordered := false
	if t, ok := tx.(sdk.TxWithUnordered); ok {
		unordered = t.GetUnordered()
	}
	e := Entry{
		Tx:        tx,
		Bytes:     bytes.Clone(bz),
		Key:       sha256.Sum256(bz),
		Signers:   signers,
		Unordered: unordered,
	}
	e.Nonce, err = sdkmempool.ChooseNonce(signers[0].Sequence, tx)
	if err != nil {
		return Entry{}, err
	}
	return e, nil
}

// extractSigners uses message signers, allowing an omitted public key when
// the authenticated account already has one.
func extractSigners(tx sdk.Tx) ([]sdkmempool.SignerData, error) {
	t, ok := tx.(signing.SigVerifiableTx)
	if !ok {
		return nil, fmt.Errorf("transaction has no signer information")
	}
	addresses, err := t.GetSigners()
	if err != nil {
		return nil, err
	}
	sigs, err := t.GetSignaturesV2()
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 || len(addresses) != len(sigs) {
		return nil, fmt.Errorf("invalid signer count")
	}
	out := make([]sdkmempool.SignerData, len(sigs))
	for i, sig := range sigs {
		out[i] = sdkmempool.NewSignerData(addresses[i], sig.Sequence)
	}
	return out, nil
}

// insert reserves storage before the caller retains any ante writes. An entry
// with an arrival number is a surviving transaction being revalidated.
func (p *Pool) insert(e Entry) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.entries[e.Key]; ok {
		return ErrDuplicate
	}
	// Refuse replacement before check-state writes. Different-nonce updates are
	// ordinary pending transactions, never silently coalesced.
	nonce := senderNonce{
		sender: string(e.Signers[0].Signer),
		nonce:  e.Nonce,
	}
	if _, ok := p.nonces[nonce]; ok {
		return fmt.Errorf("sender sequence already pending")
	}
	slot := e.Lane
	if slot != LaneNormal && !p.senderFits(e) {
		slot = LaneNormal
	}
	if !p.fits(slot, len(e.Bytes)) {
		slot = LaneNormal
	}
	if !p.fits(slot, len(e.Bytes)) {
		return ErrCapacity
	}
	if e.Added == 0 {
		p.serial++
		e.Added = p.serial
	}
	e.Slot = slot
	p.entries[e.Key] = e
	p.nonces[nonce] = struct{}{}
	p.counts[slot]++
	p.bytes[slot] += int64(len(e.Bytes))
	if slot != LaneNormal {
		key := laneSender{lane: slot, sender: nonce.sender}
		usage := p.senders[key]
		usage.count++
		usage.bytes += int64(len(e.Bytes))
		p.senders[key] = usage
		if slot == LaneGovernance {
			for _, key := range voteKeys(e) {
				p.votes[key]++
			}
		}
	}
	return nil
}

// fits reports whether the storage partition has count and byte capacity.
// The caller must hold p.mu.
func (p *Pool) fits(lane int8, size int) bool {
	counts := [3]int{
		0,
		p.config.MaxTxs * GovernanceAdmissionCountShare / 10000,
		p.config.MaxTxs * CommitteeAdmissionCountShare / 10000,
	}
	counts[LaneNormal] = p.config.MaxTxs - counts[LaneGovernance] - counts[LaneCommittee]
	// The configured byte budget can reach MaxInt64; fraction avoids an
	// overflowing intermediate product while preserving floor rounding.
	limits := [3]int64{
		0,
		int64(fraction(uint64(p.config.MaxTxsBytes), GovernanceAdmissionByteShare)),
		int64(fraction(uint64(p.config.MaxTxsBytes), CommitteeAdmissionByteShare)),
	}
	limits[LaneNormal] = p.config.MaxTxsBytes - limits[LaneGovernance] - limits[LaneCommittee]
	return p.counts[lane] < counts[lane] && int64(size) <= limits[lane]-p.bytes[lane]
}

// No one authenticated sender may reserve more than a quarter of a class's
// transaction slots. Excess remains eligible for normal admission. Byte limits
// also apply to that sender independently of the count limit.
// The caller must hold p.mu.
func (p *Pool) senderFits(e Entry) bool {
	countShare, byteShare := int64(GovernanceAdmissionCountShare), int64(GovernanceAdmissionByteShare)
	if e.Lane == LaneCommittee {
		countShare, byteShare = CommitteeAdmissionCountShare, CommitteeAdmissionByteShare
	}
	maxCount := int(int64(p.config.MaxTxs) * countShare / 10000 / 4)
	if maxCount < 1 {
		maxCount = 1
	}
	maxBytes := int64(fraction(uint64(p.config.MaxTxsBytes), uint64(byteShare))) / 4
	usage := p.senders[laneSender{lane: e.Lane, sender: string(e.Signers[0].Signer)}]
	if e.Lane == LaneGovernance {
		for _, key := range voteKeys(e) {
			if p.votes[key] != 0 {
				return false
			}
		}
	}
	return usage.count < maxCount && int64(len(e.Bytes)) <= maxBytes-usage.bytes
}

func (p *Pool) RemoveBytes(bz []byte) {
	_ = p.removeKey(sha256.Sum256(bz))
}

func (p *Pool) Remove(tx sdk.Tx) error {
	bz, err := p.encode(tx)
	if err != nil {
		return err
	}
	return p.removeKey(sha256.Sum256(bz))
}

func (p *Pool) RemoveWithReason(_ context.Context, tx sdk.Tx, reason sdkmempool.RemoveReason) error {
	// Optimistic execution may finalise a proposal which never commits. Retire
	// transactions only in Handler.Commit, including those failing ante.
	if reason.Caller == sdkmempool.CallerRunTxFinalize {
		return nil
	}
	return p.Remove(tx)
}

func (p *Pool) removeKey(key [32]byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[key]
	if !ok {
		return sdkmempool.ErrTxNotFound
	}
	delete(p.entries, key)
	sender := string(e.Signers[0].Signer)
	delete(p.nonces, senderNonce{sender: sender, nonce: e.Nonce})
	p.counts[e.Slot]--
	p.bytes[e.Slot] -= int64(len(e.Bytes))
	if e.Slot != LaneNormal {
		key := laneSender{lane: e.Slot, sender: sender}
		usage := p.senders[key]
		usage.count--
		usage.bytes -= int64(len(e.Bytes))
		if usage.count == 0 {
			delete(p.senders, key)
		} else {
			p.senders[key] = usage
		}
		if e.Slot == LaneGovernance {
			for _, key := range voteKeys(e) {
				p.votes[key]--
				if p.votes[key] == 0 {
					delete(p.votes, key)
				}
			}
		}
	}
	return nil
}

// Select and SelectBy satisfy ExtMempool only. No production path calls them:
// proposal assembly and gossip use SelectEntries with their own budgets, and
// the SDK's default PrepareProposal handler is not installed. Each call copies
// the pool.
func (p *Pool) Select(_ context.Context, _ [][]byte) sdkmempool.Iterator {
	var txs []sdk.Tx
	SelectEntries(p.Snapshot(), math.MaxUint64, 0, func(e Entry, _ int8) bool {
		txs = append(txs, e.Tx)
		return true
	})
	if len(txs) == 0 {
		return nil
	}
	return &snapshotIterator{txs: txs}
}

func (p *Pool) SelectBy(ctx context.Context, txs [][]byte, f func(sdk.Tx) bool) {
	for it := p.Select(ctx, txs); it != nil; it = it.Next() {
		if !f(it.Tx()) {
			break
		}
	}
}

type snapshotIterator struct{ txs []sdk.Tx }

func (i *snapshotIterator) Tx() sdk.Tx { return i.txs[0] }

func (i *snapshotIterator) Next() sdkmempool.Iterator {
	if len(i.txs) <= 1 {
		return nil
	}
	return &snapshotIterator{txs: i.txs[1:]}
}
