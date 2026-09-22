use cosmwasm_std::{OverflowError, StdError, Uint128};
use thiserror::Error;

#[derive(Error, Debug, PartialEq)]
pub enum ContractError {
    #[error("{0}")]
    Std(#[from] StdError),

    #[error("{0}")]
    Overflow(#[from] OverflowError),

    #[error("unauthorized: {0}")]
    Unauthorized(String),

    #[error("invalid config: {0}")]
    Config(String),

    #[error("invalid schedule: {0}")]
    Schedule(String),

    #[error("batch of {0} exceeds {1} addresses")]
    BatchTooLarge(usize, usize),

    #[error("no addresses")]
    EmptyBatch,

    #[error("member {0} already registered")]
    MemberExists(String),

    #[error("issuance limit: {remaining} more in the window, {requested} requested; the batch fits at {fits_at}")]
    IssuanceLimit {
        remaining: u64,
        requested: u64,
        fits_at: u64,
    },

    #[error("unallocated balance {available} is below {needed}")]
    Unallocated { available: Uint128, needed: Uint128 },

    #[error("grant {0} not found")]
    GrantNotFound(u64),

    #[error("grantee {0} not found")]
    PersonNotFound(String),

    #[error("grant {0} is cancelled")]
    Cancelled(u64),

    #[error("grant {0} is fully released")]
    Exhausted(u64),

    #[error("grant {0} has no release address")]
    NoReleaseAddress(u64),

    #[error("grant {0}: nothing releasable")]
    NothingReleasable(u64),

    #[error("amount must be positive")]
    ZeroAmount,

    #[error("unknown reply {0}")]
    UnknownReply(u64),

    #[error("pool query returned no pool")]
    NoPool,

    #[error("bonded tokens {0} are not a whole number")]
    BadBonded(String),
}
