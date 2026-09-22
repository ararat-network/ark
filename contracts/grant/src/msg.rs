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
    /// fee_allowance is the gas each member is granted at issuance, paid by
    /// the contract, so a fresh vesting account can delegate.
    pub fee_allowance: Uint128,
    /// founding_stake is the founding seats' combined locked stake, the bloc
    /// rule's base; seat_stake is one seat, counted in a seat holder's own.
    pub founding_stake: Uint128,
    pub seat_stake: Uint128,
    pub cap: CapRule,
}

#[cw_serde]
pub enum ExecuteMsg {
    /// RegisterMembers issues the member grant to each address, registrar
    /// only. An address that already holds an account is rejected and
    /// recorded; the rest of the batch still lands.
    RegisterMembers { addresses: Vec<String> },
    /// SetReleaseAddress names the fresh address a grant's next tranche is
    /// created at, by the grantee's controller.
    SetReleaseAddress { id: u64, address: String },
    /// SetController rotates the grantee's controlling key, by the current one.
    SetController { grantee: String, controller: String },
    /// Release pays a grant whatever the cap and the bloc rule allow now.
    /// Anyone may call it.
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
        fee_allowance: Uint128,
    },
    /// AddGrant escrows a contributor grant from the unallocated balance and
    /// pays its first tranche to grantee, which becomes the grant's
    /// controller.
    AddGrant {
        grantee: String,
        amount: Uint128,
        schedule: Vec<Period>,
        seat_holder: bool,
    },
    SetController {
        grantee: String,
        controller: String,
    },
    /// CancelGrant returns what a grant has not released to the community pool.
    CancelGrant {
        id: u64,
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
    pub release_address: Option<Addr>,
    pub cancelled: bool,
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

#[cw_serde]
pub struct ReleasableResponse {
    pub id: u64,
    pub amount: Uint128,
    pub bonded: Uint128,
    pub own: Uint128,
    pub cap: Uint128,
    /// seat_allowance is the bloc rule's room for this grant, seat holders only.
    pub seat_allowance: Option<Uint128>,
}

#[cw_serde]
pub struct TotalsResponse {
    pub balance: Uint128,
    pub unallocated: Uint128,
    pub escrowed: Uint128,
    pub contributors_paid: Uint128,
    pub members_issued: u64,
    pub members_rejected: u64,
}

#[cw_serde]
pub enum MemberStatus {
    Issued { height: u64 },
    Rejected { height: u64, error: String },
}

#[cw_serde]
pub struct MemberResponse {
    pub status: Option<MemberStatus>,
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
