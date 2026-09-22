use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::{Addr, Uint128};

/// Period is one vesting period. A grant splits across a schedule in
/// proportion to parts, the rounding remainder landing in the last period.
#[cw_serde]
pub struct Period {
    /// length is the period's duration in seconds.
    pub length: u64,
    /// parts is the period's share of the grant, over the sum of all parts.
    pub parts: u64,
}

/// CapRule bounds what one person may have received from the pool: a share of
/// the bonded stake they do not hold, never above the ceiling, released in
/// whole units once a grant is larger than its allowance.
#[cw_serde]
pub struct CapRule {
    pub numerator: u64,
    pub denominator: u64,
    pub ceiling: Uint128,
    pub unit: Uint128,
}

/// IssuanceLimit bounds how many members the registrar may register in any
/// window_seconds, so a stolen registrar key costs a window's issuance, not a
/// tranche.
#[cw_serde]
pub struct IssuanceLimit {
    pub max_members: u64,
    pub window_seconds: u64,
}

/// IssuanceEntry is the registrations attempted in one block; they count
/// against the limit for window_seconds from it.
#[cw_serde]
pub struct IssuanceEntry {
    pub at: u64,
    pub count: u64,
}

/// Kind is how a grant leaves escrow: what admits a release and what it
/// becomes.
#[cw_serde]
pub enum Kind {
    /// Ownership releases by the cap and the bloc rule into a fresh vesting
    /// account on the schedule, so the coins stake and are the grantee's.
    /// fee_reserve is the gas its proposal brought for its tranches, one
    /// allowance each, not yet promised to one; release_address is where the
    /// next tranche is created, spent by it.
    Ownership {
        fee_reserve: Uint128,
        release_address: Option<Addr>,
    },
    /// Stream releases by the clock: the contract keeps the schedule and
    /// sends each period to payee once it has elapsed since start, so the
    /// unpaid rest stays cancellable. paid is the periods sent so far.
    Stream { start: u64, paid: u32, payee: Addr },
}

#[cw_serde]
pub struct InstantiateMsg {
    /// denom is the only denomination the contract pays; the plan's is anoah.
    pub denom: String,
    /// registrar may register members; none until governance sets one.
    pub registrar: Option<String>,
    pub issuance_limit: IssuanceLimit,
    /// member_grant is what every member receives, on member_schedule.
    pub member_grant: Uint128,
    pub member_schedule: Vec<Period>,
    /// fee_allowance is the gas each tranche account is granted, since
    /// nothing in it is spendable until its cliff; a grant proposal funds one
    /// per whole unit of its grant. Members pay from the period paid at
    /// registration.
    pub fee_allowance: Uint128,
    /// founding_stake is the founding seats' combined locked stake, the bloc
    /// rule's base; seat_stake is one seat, counted in a seat holder's own.
    pub founding_stake: Uint128,
    pub seat_stake: Uint128,
    pub cap: CapRule,
}

#[cw_serde]
pub enum ExecuteMsg {
    /// RegisterMembers opens the member grant for each address, registrar
    /// only: the first period is sent at once and the rest is held, paid by
    /// ReleaseMembers as its periods elapse.
    RegisterMembers { addresses: Vec<String> },
    /// ReleaseMembers pays each address the periods elapsed since it was last
    /// paid. Anyone may call it; a suspended, cancelled, or paid-out member
    /// is skipped, and a batch that pays nothing fails.
    ReleaseMembers { addresses: Vec<String> },
    /// SuspendMembers stops paying each address until it is reinstated,
    /// registrar only. Nothing moves: a cancel later settles only what had
    /// elapsed by the suspension, and a reinstatement pays everything since.
    SuspendMembers { addresses: Vec<String> },
    /// ReinstateMembers lifts a suspension, registrar only.
    ReinstateMembers { addresses: Vec<String> },
    /// SetReleaseAddress names where a grant's next release goes, by the
    /// grantee's controller: a fresh address for an ownership tranche, spent
    /// by it; any address for a stream's pay, kept.
    SetReleaseAddress { id: u64, address: String },
    /// SetController rotates the grantee's controlling key, by the current one.
    SetController { grantee: String, controller: String },
    /// Release pays a grant what its kind allows now: the cap and the bloc
    /// rule for ownership, the elapsed periods for a stream. Anyone may call
    /// it.
    Release { id: u64 },
}

/// SudoMsg is governance's surface, reached by `MsgSudoContract`.
#[cw_serde]
pub enum SudoMsg {
    SetRegistrar {
        registrar: Option<String>,
    },
    SetIssuanceLimit {
        limit: IssuanceLimit,
    },
    SetMemberGrant {
        amount: Uint128,
        schedule: Vec<Period>,
    },
    /// SetFeeAllowance sets the gas each new tranche account is granted; gas
    /// already reserved for a grant is not resized.
    SetFeeAllowance {
        amount: Uint128,
    },
    /// AddGrant escrows an ownership grant and its gas, one allowance per
    /// whole unit, and pays its first tranche to grantee, which becomes the
    /// grant's controller.
    AddGrant {
        grantee: String,
        amount: Uint128,
        schedule: Vec<Period>,
        seat_holder: bool,
    },
    /// AddStream escrows a pay stream the contract pays down itself: each
    /// period of schedule goes to grantee by bank send once it has elapsed,
    /// outside the cap and the bloc rule. Nothing pays at once.
    AddStream {
        grantee: String,
        amount: Uint128,
        schedule: Vec<Period>,
    },
    SetController {
        grantee: String,
        controller: String,
    },
    /// CancelGrant returns what a grant has not released to the community
    /// pool; a stream first pays what has elapsed.
    CancelGrant {
        id: u64,
    },
    /// CancelMembers ends each address's grant: what had elapsed by its
    /// suspension, or by now if it was not suspended, is paid, and the rest
    /// stays in the tranche as unallocated.
    CancelMembers {
        addresses: Vec<String>,
    },
    /// ReinstateMembers lifts suspensions the registrar will not.
    ReinstateMembers {
        addresses: Vec<String>,
    },
    /// VoidSuspensions lifts every suspension a registrar made up to now, in
    /// one write: the proposal that replaces a stolen key voids what it did.
    /// Its later suspensions, if it is set again, stand.
    VoidSuspensions {
        registrar: String,
    },
    /// ReturnUnallocated sends idle balance back to the community pool.
    ReturnUnallocated {
        amount: Uint128,
    },
}

#[cw_serde]
pub struct MigrateMsg {}

#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(ConfigResponse)]
    Config {},
    #[returns(GrantResponse)]
    Grant { id: u64 },
    #[returns(PersonResponse)]
    Person { grantee: String },
    #[returns(ReleasableResponse)]
    Releasable { id: u64 },
    #[returns(TotalsResponse)]
    Totals {},
    #[returns(MemberResponse)]
    Member { address: String },
    #[returns(IssuanceResponse)]
    Issuance {},
}

#[cw_serde]
pub struct ConfigResponse {
    pub denom: String,
    pub registrar: Option<Addr>,
    pub issuance_limit: IssuanceLimit,
    pub member_grant: Uint128,
    pub member_schedule: Vec<Period>,
    pub fee_allowance: Uint128,
    pub founding_stake: Uint128,
    pub seat_stake: Uint128,
    pub cap: CapRule,
}

#[cw_serde]
pub struct GrantResponse {
    pub id: u64,
    pub grantee: Addr,
    pub amount: Uint128,
    pub released: Uint128,
    pub remaining: Uint128,
    pub schedule: Vec<Period>,
    pub cancelled: bool,
    pub kind: Kind,
}

#[cw_serde]
pub struct PersonResponse {
    pub grantee: Addr,
    pub controller: Addr,
    pub seat_holder: bool,
    pub released: Uint128,
    /// own is everything counted against the person: seat and released here.
    pub own: Uint128,
}

/// ReleasableResponse is what release would pay now and the figures behind
/// it, by the grant's kind.
#[cw_serde]
pub struct ReleasableResponse {
    pub id: u64,
    pub amount: Uint128,
    pub rule: Rule,
}

#[cw_serde]
pub enum Rule {
    /// Cap is the ownership rule against bonded stake read now;
    /// seat_allowance is the bloc rule's room for this grant, seat holders
    /// only.
    Cap {
        bonded: Uint128,
        own: Uint128,
        cap: Uint128,
        seat_allowance: Option<Uint128>,
    },
    /// Clock is a stream's position: periods elapsed since start, periods
    /// paid, and when the next elapses, none once all have.
    Clock {
        start: u64,
        elapsed: u32,
        paid: u32,
        next_at: Option<u64>,
    },
}

#[cw_serde]
pub struct TotalsResponse {
    pub balance: Uint128,
    pub unallocated: Uint128,
    pub escrowed: Uint128,
    /// fees_reserved is the gas grants still hold for their tranches.
    pub fees_reserved: Uint128,
    /// fees_promised is every allowance granted, drawn or not.
    pub fees_promised: Uint128,
    pub contributors_paid: Uint128,
    /// members_paid is what member grants have paid out, the first periods
    /// at registration included.
    pub members_paid: Uint128,
    pub members_issued: u64,
    pub members_cancelled: u64,
}

/// Member is one member grant, the schedule it registered under and how far
/// the contract has paid it down. It is paid to its own address, the first
/// period at registration and the rest by the clock from start.
#[cw_serde]
pub struct Member {
    pub start: u64,
    pub amount: Uint128,
    pub schedule: Vec<Period>,
    pub released: Uint128,
    pub remaining: Uint128,
    /// paid is the periods sent so far.
    pub paid: u32,
    pub suspended: Option<Suspension>,
    pub cancelled: bool,
}

/// Suspension is who stopped a member's pay and when; the block time is the
/// clock a cancel settles to, the height is what a void compares against.
#[cw_serde]
pub struct Suspension {
    pub at: u64,
    pub height: u64,
    pub by: Addr,
}

/// MemberResponse is a member as the contract will treat it: a voided
/// suspension reads as none. releasable is what ReleaseMembers would pay
/// the address now, elapsed and next_at its clock, read at the suspension
/// for a suspended member, since that is what a cancel settles to.
#[cw_serde]
pub struct MemberResponse {
    pub member: Option<Member>,
    pub releasable: Uint128,
    pub elapsed: u32,
    pub next_at: Option<u64>,
}

/// IssuanceResponse is the window ending now: what it holds, oldest first,
/// and what the registrar may still issue.
#[cw_serde]
pub struct IssuanceResponse {
    pub window_start: u64,
    pub issued: u64,
    pub remaining: u64,
    pub entries: Vec<IssuanceEntry>,
}
