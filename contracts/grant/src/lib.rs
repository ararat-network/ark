//! The grant contract of the NOAH distribution plan: member grants paid down
//! by the clock from registration and the contributor escrow of ownership
//! grants and pay streams, funded by governance from the community pool and
//! instructed through sudo. See README.md.

pub mod contract;
pub mod error;
pub mod msg;
pub mod proto;
pub mod rules;
pub mod state;

pub use crate::error::ContractError;
