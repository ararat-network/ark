use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Uint128};
use cw_storage_plus::{Item, Map};

use crate::msg::{CapRule, IssuanceLimit, MemberStatus, Period};

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
/// address of their first tranche.
#[cw_serde]
pub struct Person {
    pub controller: Addr,
    pub seat_holder: bool,
    pub paid_elsewhere: Uint128,
    pub released: Uint128,
}

#[cw_serde]
pub struct Grant {
    pub id: u64,
    pub grantee: Addr,
    pub amount: Uint128,
    pub released: Uint128,
    pub remaining: Uint128,
    pub schedule: Vec<Period>,
    pub release_address: Option<Addr>,
    pub cancelled: bool,
}

/// Pending rides in a submessage's payload so the reply knows what it was.
#[cw_serde]
pub enum Pending {
    Member {
        address: Addr,
    },
    Tranche {
        id: u64,
        address: Addr,
        amount: Uint128,
    },
}

/// IssuanceWindow counts registrations attempted since start; rejected ones
/// count too, since the limit bounds the registrar, not the outcome.
#[cw_serde]
pub struct IssuanceWindow {
    pub start: u64,
    pub issued: u64,
}

pub const CONFIG: Item<Config> = Item::new("config");
pub const ISSUANCE: Item<IssuanceWindow> = Item::new("issuance");
pub const NEXT_ID: Item<u64> = Item::new("next_id");
pub const GRANTS: Map<u64, Grant> = Map::new("grants");
pub const PERSONS: Map<&Addr, Person> = Map::new("persons");
/// ESCROWED is the sum of every grant's remaining amount: what the balance
/// holds that is already promised.
pub const ESCROWED: Item<Uint128> = Item::new("escrowed");
pub const CONTRIBUTORS_PAID: Item<Uint128> = Item::new("contributors_paid");
pub const MEMBERS: Map<&Addr, MemberStatus> = Map::new("members");
pub const MEMBERS_ISSUED: Item<u64> = Item::new("members_issued");
pub const MEMBERS_REJECTED: Item<u64> = Item::new("members_rejected");
pub const REPLY_SEQ: Item<u64> = Item::new("reply_seq");
