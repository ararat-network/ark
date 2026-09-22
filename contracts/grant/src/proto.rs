//! Encoders for the SDK messages the contract dispatches and the query it
//! reads. Type URLs are the SDK's; the contract is always the signer.

use cosmos_sdk_proto::cosmos::base::v1beta1::Coin as ProtoCoin;
use cosmos_sdk_proto::cosmos::distribution::v1beta1::MsgFundCommunityPool;
use cosmos_sdk_proto::cosmos::feegrant::v1beta1::{BasicAllowance, MsgGrantAllowance};
use cosmos_sdk_proto::cosmos::staking::v1beta1::{QueryPoolRequest, QueryPoolResponse};
use cosmos_sdk_proto::cosmos::vesting::v1beta1::{
    MsgCreatePeriodicVestingAccount, Period as ProtoPeriod,
};
use cosmos_sdk_proto::Any;
use cosmwasm_std::{AnyMsg, Binary, CosmosMsg, QuerierWrapper, StdError, StdResult, Uint128};
use prost::Message;

use crate::error::ContractError;
use crate::msg::Period;

pub const POOL_QUERY_PATH: &str = "/cosmos.staking.v1beta1.Query/Pool";

fn coin(denom: &str, amount: Uint128) -> ProtoCoin {
    ProtoCoin {
        denom: denom.to_string(),
        amount: amount.to_string(),
    }
}

fn any(type_url: &str, value: Vec<u8>) -> CosmosMsg {
    CosmosMsg::Any(AnyMsg {
        type_url: type_url.to_string(),
        value: Binary::from(value),
    })
}

/// create_vesting_account is the SDK's periodic vesting account creation,
/// funded by the contract, with the schedule's amounts already split.
pub fn create_vesting_account(
    contract: &str,
    to: &str,
    denom: &str,
    start_time: i64,
    schedule: &[Period],
    amounts: &[Uint128],
) -> CosmosMsg {
    let vesting_periods = schedule
        .iter()
        .zip(amounts)
        .map(|(p, amount)| ProtoPeriod {
            length: p.length as i64,
            // split refuses a zero share, so every period carries a coin;
            // the chain rejects a period without one.
            amount: vec![coin(denom, *amount)],
        })
        .collect();
    let msg = MsgCreatePeriodicVestingAccount {
        from_address: contract.to_string(),
        to_address: to.to_string(),
        start_time,
        vesting_periods,
    };
    any(
        "/cosmos.vesting.v1beta1.MsgCreatePeriodicVestingAccount",
        msg.encode_to_vec(),
    )
}

/// grant_fee_allowance gives a member a basic allowance from the contract so
/// their first delegation can pay its gas.
pub fn grant_fee_allowance(
    contract: &str,
    grantee: &str,
    denom: &str,
    amount: Uint128,
) -> CosmosMsg {
    let allowance = BasicAllowance {
        spend_limit: vec![coin(denom, amount)],
        expiration: None,
    };
    let msg = MsgGrantAllowance {
        granter: contract.to_string(),
        grantee: grantee.to_string(),
        allowance: Some(Any {
            type_url: "/cosmos.feegrant.v1beta1.BasicAllowance".to_string(),
            value: allowance.encode_to_vec(),
        }),
    };
    any(
        "/cosmos.feegrant.v1beta1.MsgGrantAllowance",
        msg.encode_to_vec(),
    )
}

/// fund_community_pool returns coins to the pool they came from.
pub fn fund_community_pool(contract: &str, denom: &str, amount: Uint128) -> CosmosMsg {
    let msg = MsgFundCommunityPool {
        amount: vec![coin(denom, amount)],
        depositor: contract.to_string(),
    };
    any(
        "/cosmos.distribution.v1beta1.MsgFundCommunityPool",
        msg.encode_to_vec(),
    )
}

/// bonded_tokens reads total bonded stake from the staking pool, the number
/// every rule measures against.
pub fn bonded_tokens(querier: &QuerierWrapper) -> Result<Uint128, ContractError> {
    let raw = querier.query_grpc(
        POOL_QUERY_PATH.to_string(),
        Binary::from(QueryPoolRequest {}.encode_to_vec()),
    )?;
    let response = QueryPoolResponse::decode(raw.as_slice())
        .map_err(|e| StdError::generic_err(e.to_string()))?;
    let pool = response.pool.ok_or(ContractError::NoPool)?;
    parse_uint(&pool.bonded_tokens)
}

fn parse_uint(s: &str) -> Result<Uint128, ContractError> {
    s.parse::<u128>()
        .map(Uint128::new)
        .map_err(|_| ContractError::BadBonded(s.to_string()))
}

/// decode_pool_response is exposed for tests that stub the query.
pub fn encode_pool_response(bonded: Uint128, not_bonded: Uint128) -> StdResult<Binary> {
    let response = QueryPoolResponse {
        pool: Some(cosmos_sdk_proto::cosmos::staking::v1beta1::Pool {
            not_bonded_tokens: not_bonded.to_string(),
            bonded_tokens: bonded.to_string(),
        }),
    };
    Ok(Binary::from(response.encode_to_vec()))
}
