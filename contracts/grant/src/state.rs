use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Uint128};
use cw_storage_plus::{Item, Map};

use crate::msg::{CapRule, IssuanceEntry, IssuanceLimit, Kind, Member, Period};

#[cw_serde]
pub struct Config {
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

/// Person is one grantee across every grant made to them here, keyed by the
/// address of their first grant.
#[cw_serde]
pub struct Person {
    pub controller: Addr,
    pub seat_holder: bool,
    /// released is what ownership grants have paid them, assumed bonded.
    /// Stream pay is spent, not held, and does not count.
    pub released: Uint128,
}

/// Grant is one escrow entry: what governance put in, what has left, and the
/// kind that decides how the rest leaves.
#[cw_serde]
pub struct Grant {
    pub id: u64,
    pub grantee: Addr,
    pub amount: Uint128,
    pub released: Uint128,
    pub remaining: Uint128,
    pub schedule: Vec<Period>,
    pub cancelled: bool,
    pub kind: Kind,
}

/// Pending rides in a submessage's payload so the reply knows what it was.
#[cw_serde]
pub enum Pending {
    Tranche {
        id: u64,
        address: Addr,
        amount: Uint128,
    },
}

pub const CONFIG: Item<Config> = Item::new("config");
/// ISSUANCE is the registrations within the window, oldest first and merged
/// by block time. Every entry holds at least one, so the log never exceeds
/// max_members entries.
pub const ISSUANCE: Item<Vec<IssuanceEntry>> = Item::new("issuance");
pub const NEXT_ID: Item<u64> = Item::new("next_id");
pub const GRANTS: Map<u64, Grant> = Map::new("grants");
pub const PERSONS: Map<&Addr, Person> = Map::new("persons");
/// ESCROWED is the sum of every grant's remaining amount: what the balance
/// holds that is already promised.
pub const ESCROWED: Item<Uint128> = Item::new("escrowed");
/// FEES_RESERVED is the sum of every ownership grant's fee_reserve.
pub const FEES_RESERVED: Item<Uint128> = Item::new("fees_reserved");
/// FEES_PROMISED is the sum of every fee allowance granted. The contract
/// cannot see an allowance being drawn, so a promise counts for good.
pub const FEES_PROMISED: Item<Uint128> = Item::new("fees_promised");
pub const CONTRIBUTORS_PAID: Item<Uint128> = Item::new("contributors_paid");
/// MEMBERS is the member roll, keyed by the address each grant pays. It is
/// kept apart from GRANTS so the seat pool's scan never crosses it.
pub const MEMBERS: Map<&Addr, Member> = Map::new("members");
pub const MEMBERS_PAID: Item<Uint128> = Item::new("members_paid");
pub const MEMBERS_ISSUED: Item<u64> = Item::new("members_issued");
pub const MEMBERS_CANCELLED: Item<u64> = Item::new("members_cancelled");
/// VOIDED is the height up to which each registrar's suspensions are void,
/// checked wherever a suspension is read rather than swept over the roll.
pub const VOIDED: Map<&Addr, u64> = Map::new("voided");
pub const REPLY_SEQ: Item<u64> = Item::new("reply_seq");
