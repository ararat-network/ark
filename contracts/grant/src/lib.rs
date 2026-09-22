//! The grant contract of the NOAH distribution plan: member grants issued at
//! registration and the contributor escrow, funded by governance from the
//! community pool and instructed through sudo. See README.md.

pub mod contract;
pub mod error;
pub mod msg;
pub mod proto;
pub mod rules;
pub mod state;

pub use crate::error::ContractError;
