use cosmwasm_std::{
    coins, entry_point, from_json, to_json_binary, Addr, BankMsg, Binary, CosmosMsg, Deps, DepsMut,
    Env, Event, MessageInfo, Order, Reply, Response, StdResult, Storage, SubMsg, SubMsgResult,
    Uint128,
};
use cw2::set_contract_version;

use crate::error::ContractError;
use crate::msg::{
    ConfigResponse, ExecuteMsg, GrantResponse, InstantiateMsg, IssuanceEntry, IssuanceResponse,
    Kind, Member, MemberResponse, MigrateMsg, PersonResponse, QueryMsg, ReleasableResponse, Rule,
    SudoMsg, Suspension, TotalsResponse,
};
use crate::proto;
use crate::rules;
use crate::state::{
    Config, Grant, Pending, Person, CONFIG, CONTRIBUTORS_PAID, ESCROWED, FEES_PROMISED,
    FEES_RESERVED, GRANTS, ISSUANCE, MEMBERS, MEMBERS_CANCELLED, MEMBERS_ISSUED, MEMBERS_PAID,
    NEXT_ID, PERSONS, REPLY_SEQ, VOIDED,
};

const CONTRACT_NAME: &str = "crates.io:ark-grant";
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

/// MAX_BATCH bounds one call over member addresses.
pub const MAX_BATCH: usize = 100;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;
    if msg.denom.is_empty() {
        return Err(ContractError::Config("denom must be set".into()));
    }
    if msg.member_grant.is_zero() {
        return Err(ContractError::Config(
            "member grant must be positive".into(),
        ));
    }
    rules::validate_schedule(&msg.member_schedule)?;
    // The grant must give every period a coin, or registration would fail.
    rules::split(msg.member_grant, &msg.member_schedule)?;
    rules::validate_issuance_limit(&msg.issuance_limit)?;
    rules::validate_cap(&msg.cap)?;
    let registrar = msg
        .registrar
        .map(|r| deps.api.addr_validate(&r))
        .transpose()?;
    ISSUANCE.save(deps.storage, &Vec::new())?;
    CONFIG.save(
        deps.storage,
        &Config {
            denom: msg.denom,
            registrar,
            issuance_limit: msg.issuance_limit,
            member_grant: msg.member_grant,
            member_schedule: msg.member_schedule,
            fee_allowance: msg.fee_allowance,
            founding_stake: msg.founding_stake,
            seat_stake: msg.seat_stake,
            cap: msg.cap,
        },
    )?;
    NEXT_ID.save(deps.storage, &1)?;
    ESCROWED.save(deps.storage, &Uint128::zero())?;
    FEES_PROMISED.save(deps.storage, &Uint128::zero())?;
    FEES_RESERVED.save(deps.storage, &Uint128::zero())?;
    CONTRIBUTORS_PAID.save(deps.storage, &Uint128::zero())?;
    MEMBERS_PAID.save(deps.storage, &Uint128::zero())?;
    MEMBERS_ISSUED.save(deps.storage, &0)?;
    MEMBERS_CANCELLED.save(deps.storage, &0)?;
    REPLY_SEQ.save(deps.storage, &0)?;
    Ok(Response::new().add_attribute("action", "instantiate"))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn migrate(deps: DepsMut, _env: Env, _msg: MigrateMsg) -> Result<Response, ContractError> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;
    Ok(Response::new().add_attribute("action", "migrate"))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, ContractError> {
    match msg {
        ExecuteMsg::RegisterMembers { addresses } => register_members(deps, env, info, addresses),
        ExecuteMsg::ReleaseMembers { addresses } => release_members(deps, env, addresses),
        ExecuteMsg::SuspendMembers { addresses } => suspend_members(deps, env, info, addresses),
        ExecuteMsg::ReinstateMembers { addresses } => {
            registrar_only(&CONFIG.load(deps.storage)?, &info)?;
            reinstate_members(deps, addresses)
        }
        ExecuteMsg::SetReleaseAddress { id, address } => {
            set_release_address(deps, info, id, address)
        }
        ExecuteMsg::SetController {
            grantee,
            controller,
        } => {
            let grantee = deps.api.addr_validate(&grantee)?;
            let person = PERSONS
                .may_load(deps.storage, &grantee)?
                .ok_or_else(|| ContractError::PersonNotFound(grantee.to_string()))?;
            if person.controller != info.sender {
                return Err(ContractError::Unauthorized("not the controller".into()));
            }
            set_controller(deps, grantee, person, controller)
        }
        ExecuteMsg::Release { id } => release(deps, env, id),
    }
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn sudo(deps: DepsMut, env: Env, msg: SudoMsg) -> Result<Response, ContractError> {
    match msg {
        SudoMsg::SetRegistrar { registrar } => {
            let registrar = registrar.map(|r| deps.api.addr_validate(&r)).transpose()?;
            CONFIG.update(deps.storage, |mut c| -> StdResult<_> {
                c.registrar = registrar.clone();
                Ok(c)
            })?;
            Ok(
                Response::new().add_event(Event::new("ark-grant/registrar_set").add_attribute(
                    "registrar",
                    registrar.map(|r| r.to_string()).unwrap_or_default(),
                )),
            )
        }
        SudoMsg::SetIssuanceLimit { limit } => {
            rules::validate_issuance_limit(&limit)?;
            CONFIG.update(deps.storage, |mut c| -> StdResult<_> {
                c.issuance_limit = limit.clone();
                Ok(c)
            })?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/issuance_limit_set")
                    .add_attribute("max_members", limit.max_members.to_string())
                    .add_attribute("window_seconds", limit.window_seconds.to_string()),
            ))
        }
        SudoMsg::SetMemberGrant { amount, schedule } => {
            if amount.is_zero() {
                return Err(ContractError::ZeroAmount);
            }
            rules::validate_schedule(&schedule)?;
            rules::split(amount, &schedule)?;
            CONFIG.update(deps.storage, |mut c| -> StdResult<_> {
                c.member_grant = amount;
                c.member_schedule = schedule;
                Ok(c)
            })?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/member_grant_set").add_attribute("amount", amount),
            ))
        }
        SudoMsg::SetFeeAllowance { amount } => {
            CONFIG.update(deps.storage, |mut c| -> StdResult<_> {
                c.fee_allowance = amount;
                Ok(c)
            })?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/fee_allowance_set").add_attribute("amount", amount),
            ))
        }
        SudoMsg::AddGrant {
            grantee,
            amount,
            schedule,
            seat_holder,
            release_address,
        } => add_grant(
            deps,
            env,
            grantee,
            amount,
            schedule,
            seat_holder,
            release_address,
        ),
        SudoMsg::AddStream {
            grantee,
            amount,
            schedule,
        } => add_stream(deps, env, grantee, amount, schedule),
        SudoMsg::SetController {
            grantee,
            controller,
        } => {
            let grantee = deps.api.addr_validate(&grantee)?;
            let person = PERSONS
                .may_load(deps.storage, &grantee)?
                .ok_or_else(|| ContractError::PersonNotFound(grantee.to_string()))?;
            set_controller(deps, grantee, person, controller)
        }
        SudoMsg::CancelGrant { id } => cancel_grant(deps, env, id),
        SudoMsg::CancelMembers { addresses } => cancel_members(deps, env, addresses),
        SudoMsg::ReinstateMembers { addresses } => reinstate_members(deps, addresses),
        SudoMsg::VoidSuspensions { registrar } => {
            let registrar = deps.api.addr_validate(&registrar)?;
            VOIDED.save(deps.storage, &registrar, &env.block.height)?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/suspensions_voided")
                    .add_attribute("registrar", registrar.as_str())
                    .add_attribute("height", env.block.height.to_string()),
            ))
        }
        SudoMsg::ReturnUnallocated { amount } => {
            if amount.is_zero() {
                return Err(ContractError::ZeroAmount);
            }
            let config = CONFIG.load(deps.storage)?;
            let available = unallocated(deps.as_ref(), &env, &config)?;
            if amount > available {
                return Err(ContractError::Unallocated {
                    available,
                    needed: amount,
                });
            }
            Ok(Response::new()
                .add_message(proto::fund_community_pool(
                    env.contract.address.as_str(),
                    &config.denom,
                    amount,
                ))
                .add_event(Event::new("ark-grant/returned").add_attribute("amount", amount)))
        }
    }
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn reply(deps: DepsMut, env: Env, msg: Reply) -> Result<Response, ContractError> {
    let pending: Pending =
        from_json(&msg.payload).map_err(|_| ContractError::UnknownReply(msg.id))?;
    let config = CONFIG.load(deps.storage)?;
    match (pending, msg.result) {
        (
            Pending::Tranche {
                id,
                address,
                amount,
            },
            SubMsgResult::Ok(_),
        ) => {
            let mut grant = GRANTS.load(deps.storage, id)?;
            // Only tranche() carries this payload, for ownership grants.
            let Kind::Ownership {
                fee_reserve: reserve,
                ..
            } = grant.kind
            else {
                return Err(ContractError::NotOwnership(id));
            };
            grant.released = grant.released.checked_add(amount)?;
            grant.remaining = grant.remaining.checked_sub(amount)?;
            // The tranche's gas is the grant's own reserve, spread over the
            // tranches it can still take, so a raised allowance neither
            // reaches a grant reserved at the old rate nor starves its last
            // tranches. The last returns what the reserve did not need.
            let gas = rules::tranche_gas(
                reserve,
                config.fee_allowance,
                grant.remaining,
                config.cap.unit,
            )?;
            let reserve = reserve.checked_sub(gas)?;
            let unused = if grant.remaining.is_zero() {
                reserve
            } else {
                Uint128::zero()
            };
            grant.kind = Kind::Ownership {
                fee_reserve: reserve.checked_sub(unused)?,
                release_address: None,
            };
            GRANTS.save(deps.storage, id, &grant)?;
            PERSONS.update(
                deps.storage,
                &grant.grantee,
                |p| -> Result<_, ContractError> {
                    let mut p =
                        p.ok_or_else(|| ContractError::PersonNotFound(grant.grantee.to_string()))?;
                    p.released = p.released.checked_add(amount)?;
                    Ok(p)
                },
            )?;
            ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
                Ok(e.checked_sub(amount)?)
            })?;
            CONTRIBUTORS_PAID.update(deps.storage, |t| -> Result<_, ContractError> {
                Ok(t.checked_add(amount)?)
            })?;
            FEES_RESERVED.update(deps.storage, |r| -> Result<_, ContractError> {
                Ok(r.checked_sub(gas)?.checked_sub(unused)?)
            })?;
            let mut response = Response::new()
                .add_event(
                    Event::new("ark-grant/released")
                        .add_attribute("id", id.to_string())
                        .add_attribute("address", address.as_str())
                        .add_attribute("amount", amount)
                        .add_attribute("remaining", grant.remaining)
                        .add_attribute("gas_returned", unused),
                )
                .add_messages(promise_allowance(
                    deps.storage,
                    &config,
                    &env,
                    &address,
                    gas,
                )?);
            if !unused.is_zero() {
                response = response.add_message(proto::fund_community_pool(
                    env.contract.address.as_str(),
                    &config.denom,
                    unused,
                ));
            }
            Ok(response)
        }
        (Pending::Tranche { id, address, .. }, SubMsgResult::Err(error)) => {
            GRANTS.update(deps.storage, id, |g| -> Result<_, ContractError> {
                let mut g = g.ok_or(ContractError::GrantNotFound(id))?;
                if let Kind::Ownership {
                    release_address, ..
                } = &mut g.kind
                {
                    *release_address = None;
                }
                Ok(g)
            })?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/release_rejected")
                    .add_attribute("id", id.to_string())
                    .add_attribute("address", address.as_str())
                    .add_attribute("error", error),
            ))
        }
    }
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Config {} => {
            let c = CONFIG.load(deps.storage)?;
            to_json_binary(&ConfigResponse {
                denom: c.denom,
                registrar: c.registrar,
                issuance_limit: c.issuance_limit,
                member_grant: c.member_grant,
                member_schedule: c.member_schedule,
                fee_allowance: c.fee_allowance,
                founding_stake: c.founding_stake,
                seat_stake: c.seat_stake,
                cap: c.cap,
            })
        }
        QueryMsg::Grant { id } => {
            let g = GRANTS.load(deps.storage, id)?;
            to_json_binary(&grant_response(g))
        }
        QueryMsg::Person { grantee } => {
            let grantee = deps.api.addr_validate(&grantee)?;
            let p = PERSONS.load(deps.storage, &grantee)?;
            let config = CONFIG.load(deps.storage)?;
            to_json_binary(&PersonResponse {
                grantee,
                controller: p.controller.clone(),
                seat_holder: p.seat_holder,
                released: p.released,
                own: own(&config, &p)
                    .map_err(|e| cosmwasm_std::StdError::generic_err(e.to_string()))?,
            })
        }
        QueryMsg::Releasable { id } => {
            let config = CONFIG.load(deps.storage)?;
            let grant = GRANTS.load(deps.storage, id)?;
            let person = PERSONS.load(deps.storage, &grant.grantee)?;
            let r = releasable(deps, &env, &config, &grant, &person)
                .map_err(|e| cosmwasm_std::StdError::generic_err(e.to_string()))?;
            to_json_binary(&r)
        }
        QueryMsg::Totals {} => {
            let config = CONFIG.load(deps.storage)?;
            let balance = deps
                .querier
                .query_balance(env.contract.address, &config.denom)?
                .amount;
            let escrowed = ESCROWED.load(deps.storage)?;
            let fees_reserved = FEES_RESERVED.load(deps.storage)?;
            let fees_promised = FEES_PROMISED.load(deps.storage)?;
            to_json_binary(&TotalsResponse {
                balance,
                unallocated: balance
                    .saturating_sub(escrowed)
                    .saturating_sub(fees_reserved)
                    .saturating_sub(fees_promised),
                escrowed,
                fees_reserved,
                fees_promised,
                contributors_paid: CONTRIBUTORS_PAID.load(deps.storage)?,
                members_paid: MEMBERS_PAID.load(deps.storage)?,
                members_issued: MEMBERS_ISSUED.load(deps.storage)?,
                members_cancelled: MEMBERS_CANCELLED.load(deps.storage)?,
            })
        }
        QueryMsg::Member { address } => {
            let address = deps.api.addr_validate(&address)?;
            let Some(mut m) = MEMBERS.may_load(deps.storage, &address)? else {
                return to_json_binary(&MemberResponse {
                    member: None,
                    releasable: Uint128::zero(),
                    elapsed: 0,
                    next_at: None,
                });
            };
            let suspended = suspension(deps.storage, &m)?;
            m.suspended = suspended.clone();
            let until = suspended
                .as_ref()
                .map_or(env.block.time.seconds(), |s| s.at);
            let (elapsed, next_at) = rules::elapsed(&m.schedule, m.start, until);
            let releasable = if m.cancelled || m.remaining.is_zero() || suspended.is_some() {
                Uint128::zero()
            } else {
                rules::accrued(m.amount, &m.schedule, m.paid, elapsed)
                    .map_err(|e| cosmwasm_std::StdError::generic_err(e.to_string()))?
            };
            to_json_binary(&MemberResponse {
                member: Some(m),
                releasable,
                elapsed,
                next_at,
            })
        }
        QueryMsg::Issuance {} => {
            let config = CONFIG.load(deps.storage)?;
            let entries = issuance_window(deps.storage, &env, &config)?;
            let issued = rules::issued(&entries);
            to_json_binary(&IssuanceResponse {
                window_start: env
                    .block
                    .time
                    .seconds()
                    .saturating_sub(config.issuance_limit.window_seconds),
                issued,
                remaining: config.issuance_limit.max_members.saturating_sub(issued),
                entries,
            })
        }
    }
}

/// issuance_window is the issuance log as of now, aged-out entries dropped.
/// Nothing is written; the caller decides.
fn issuance_window(
    storage: &dyn cosmwasm_std::Storage,
    env: &Env,
    config: &Config,
) -> StdResult<Vec<IssuanceEntry>> {
    let entries = ISSUANCE.load(storage)?;
    Ok(rules::in_window(
        entries,
        env.block.time.seconds(),
        config.issuance_limit.window_seconds,
    ))
}

fn grant_response(g: Grant) -> GrantResponse {
    GrantResponse {
        id: g.id,
        grantee: g.grantee,
        amount: g.amount,
        released: g.released,
        remaining: g.remaining,
        schedule: g.schedule,
        cancelled: g.cancelled,
        kind: g.kind,
    }
}

/// unallocated is the balance not yet promised to a grant.
/// free is the balance beyond what grants hold, their coins and their gas:
/// what a grant may draw, since it arrives with its own spend.
fn free(deps: Deps, env: &Env, config: &Config) -> Result<Uint128, ContractError> {
    let balance = deps
        .querier
        .query_balance(env.contract.address.clone(), &config.denom)?
        .amount;
    Ok(balance
        .saturating_sub(ESCROWED.load(deps.storage)?)
        .saturating_sub(FEES_RESERVED.load(deps.storage)?))
}

/// unallocated is free less the gas promised: what registration may issue
/// and governance may return. A promise stays counted once drawn, so this
/// reads low by the gas members have spent.
fn unallocated(deps: Deps, env: &Env, config: &Config) -> Result<Uint128, ContractError> {
    Ok(free(deps, env, config)?.saturating_sub(FEES_PROMISED.load(deps.storage)?))
}

/// own is everything counted against a person: the seat if they hold one
/// and what this contract has released to them, all assumed bonded.
fn own(config: &Config, person: &Person) -> Result<Uint128, ContractError> {
    let seat = if person.seat_holder {
        config.seat_stake
    } else {
        Uint128::zero()
    };
    Ok(seat.checked_add(person.released)?)
}

fn next_reply_id(deps: &mut DepsMut) -> StdResult<u64> {
    REPLY_SEQ.update(deps.storage, |n| -> StdResult<_> { Ok(n + 1) })
}

/// registrar_only admits the configured registrar and nobody else.
fn registrar_only(config: &Config, info: &MessageInfo) -> Result<(), ContractError> {
    match &config.registrar {
        Some(r) if *r == info.sender => Ok(()),
        _ => Err(ContractError::Unauthorized("not the registrar".into())),
    }
}

/// batch validates one call's addresses: some, at most MAX_BATCH.
fn batch(deps: Deps, addresses: &[String]) -> Result<Vec<Addr>, ContractError> {
    if addresses.is_empty() {
        return Err(ContractError::EmptyBatch);
    }
    if addresses.len() > MAX_BATCH {
        return Err(ContractError::BatchTooLarge(addresses.len(), MAX_BATCH));
    }
    addresses
        .iter()
        .map(|a| Ok(deps.api.addr_validate(a)?))
        .collect()
}

/// suspension is a member's suspension as the contract treats it: none once
/// the registrar that made it has been voided up to its height.
fn suspension(storage: &dyn Storage, member: &Member) -> StdResult<Option<Suspension>> {
    let Some(s) = &member.suspended else {
        return Ok(None);
    };
    Ok(match VOIDED.may_load(storage, &s.by)? {
        Some(voided) if s.height <= voided => None,
        _ => Some(s.clone()),
    })
}

/// live_member is a member that can still be paid.
fn live_member(storage: &dyn Storage, address: &Addr) -> Result<Member, ContractError> {
    let m = MEMBERS
        .may_load(storage, address)?
        .ok_or_else(|| ContractError::MemberNotFound(address.to_string()))?;
    if m.cancelled {
        return Err(ContractError::MemberCancelled(address.to_string()));
    }
    if m.remaining.is_zero() {
        return Err(ContractError::MemberPaidOut(address.to_string()));
    }
    Ok(m)
}

/// settle_members moves the totals for member pay that has left escrow.
fn settle_members(storage: &mut dyn Storage, amount: Uint128) -> Result<(), ContractError> {
    ESCROWED.update(storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_sub(amount)?)
    })?;
    MEMBERS_PAID.update(storage, |p| -> Result<_, ContractError> {
        Ok(p.checked_add(amount)?)
    })?;
    Ok(())
}

fn register_members(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    addresses: Vec<String>,
) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    registrar_only(&config, &info)?;
    // Addresses first, so a bad batch is reported as such before its cost.
    let members = batch(deps.as_ref(), &addresses)?;
    for (i, address) in members.iter().enumerate() {
        if MEMBERS.has(deps.storage, address) || members[..i].contains(address) {
            return Err(ContractError::MemberExists(address.to_string()));
        }
    }
    // The window bounds what a registrar key can issue before governance can
    // replace it.
    let now = env.block.time.seconds();
    let mut entries = issuance_window(deps.storage, &env, &config)?;
    let requested = members.len() as u64;
    match rules::fits_at(&entries, now, &config.issuance_limit, requested) {
        Some(at) if at <= now => {}
        Some(at) => {
            return Err(ContractError::IssuanceLimit {
                remaining: config
                    .issuance_limit
                    .max_members
                    .saturating_sub(rules::issued(&entries)),
                requested,
                fits_at: at,
            })
        }
        // Only when the limit is under the batch, so it fits usize.
        None => {
            return Err(ContractError::BatchTooLarge(
                members.len(),
                config.issuance_limit.max_members as usize,
            ))
        }
    }
    match entries.last_mut() {
        Some(last) if last.at == now => last.count = last.count.saturating_add(requested),
        _ => entries.push(IssuanceEntry {
            at: now,
            count: requested,
        }),
    }
    ISSUANCE.save(deps.storage, &entries)?;
    let count = Uint128::new(members.len() as u128);
    let needed = config.member_grant.checked_mul(count)?;
    let available = unallocated(deps.as_ref(), &env, &config)?;
    if needed > available {
        return Err(ContractError::Unallocated { available, needed });
    }

    // The first period pays at registration whatever its length: a fresh
    // address has nothing to pay the gas for a claim with.
    let upfront = rules::split(config.member_grant, &config.member_schedule)?[0];
    let mut response = Response::new().add_attribute("action", "register_members");
    for address in &members {
        MEMBERS.save(
            deps.storage,
            address,
            &Member {
                start: now,
                amount: config.member_grant,
                schedule: config.member_schedule.clone(),
                released: upfront,
                remaining: config.member_grant.checked_sub(upfront)?,
                paid: 1,
                suspended: None,
                cancelled: false,
            },
        )?;
        response = response
            .add_message(send(&config, address, upfront))
            .add_event(
                Event::new("ark-grant/member_issued")
                    .add_attribute("address", address.as_str())
                    .add_attribute("paid", upfront),
            );
    }
    let paid = upfront.checked_mul(count)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_add(needed.checked_sub(paid)?)?)
    })?;
    MEMBERS_PAID.update(deps.storage, |p| -> Result<_, ContractError> {
        Ok(p.checked_add(paid)?)
    })?;
    MEMBERS_ISSUED.update(deps.storage, |n| -> StdResult<_> {
        Ok(n + members.len() as u64)
    })?;
    Ok(response)
}

/// release_members pays each address the periods elapsed since it was last
/// paid, by anyone. A member with nothing to pay is skipped rather than
/// refused, so a cranker can sweep a roll; a batch that pays nobody fails.
fn release_members(
    deps: DepsMut,
    env: Env,
    addresses: Vec<String>,
) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    let now = env.block.time.seconds();
    let mut total = Uint128::zero();
    let mut response = Response::new().add_attribute("action", "release_members");
    for address in batch(deps.as_ref(), &addresses)? {
        let mut m = MEMBERS
            .may_load(deps.storage, &address)?
            .ok_or_else(|| ContractError::MemberNotFound(address.to_string()))?;
        if m.cancelled || m.remaining.is_zero() || suspension(deps.storage, &m)?.is_some() {
            continue;
        }
        let (elapsed, _) = rules::elapsed(&m.schedule, m.start, now);
        let amount = rules::accrued(m.amount, &m.schedule, m.paid, elapsed)?;
        if amount.is_zero() {
            continue;
        }
        let periods = elapsed - m.paid;
        m.released = m.released.checked_add(amount)?;
        m.remaining = m.remaining.checked_sub(amount)?;
        m.paid = elapsed;
        MEMBERS.save(deps.storage, &address, &m)?;
        total = total.checked_add(amount)?;
        response = response
            .add_message(send(&config, &address, amount))
            .add_event(
                Event::new("ark-grant/member_paid")
                    .add_attribute("address", address.as_str())
                    .add_attribute("amount", amount)
                    .add_attribute("periods", periods.to_string())
                    .add_attribute("remaining", m.remaining),
            );
    }
    if total.is_zero() {
        return Err(ContractError::NothingDue);
    }
    settle_members(deps.storage, total)?;
    Ok(response)
}

/// suspend_members stops each address's pay, registrar only. The earliest
/// suspension stands: a later one would move what a cancel settles.
fn suspend_members(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    addresses: Vec<String>,
) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    registrar_only(&config, &info)?;
    let mut response = Response::new().add_attribute("action", "suspend_members");
    for address in batch(deps.as_ref(), &addresses)? {
        let mut m = live_member(deps.storage, &address)?;
        if suspension(deps.storage, &m)?.is_some() {
            return Err(ContractError::MemberSuspended(address.to_string()));
        }
        m.suspended = Some(Suspension {
            at: env.block.time.seconds(),
            height: env.block.height,
            by: info.sender.clone(),
        });
        MEMBERS.save(deps.storage, &address, &m)?;
        response = response.add_event(
            Event::new("ark-grant/member_suspended").add_attribute("address", address.as_str()),
        );
    }
    Ok(response)
}

/// reinstate_members lifts each address's suspension; the next release pays
/// everything elapsed meanwhile. The caller has checked who asks.
fn reinstate_members(deps: DepsMut, addresses: Vec<String>) -> Result<Response, ContractError> {
    let mut response = Response::new().add_attribute("action", "reinstate_members");
    for address in batch(deps.as_ref(), &addresses)? {
        let mut m = live_member(deps.storage, &address)?;
        if suspension(deps.storage, &m)?.is_none() {
            return Err(ContractError::MemberNotSuspended(address.to_string()));
        }
        m.suspended = None;
        MEMBERS.save(deps.storage, &address, &m)?;
        response = response.add_event(
            Event::new("ark-grant/member_reinstated").add_attribute("address", address.as_str()),
        );
    }
    Ok(response)
}

/// cancel_members ends each address's grant. What had elapsed is the
/// member's, since anyone could have released it before the vote executed,
/// up to a suspension, after which nobody could. The rest leaves escrow and
/// stays in the balance: it is the tranche's again, for the next member.
fn cancel_members(
    deps: DepsMut,
    env: Env,
    addresses: Vec<String>,
) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    let now = env.block.time.seconds();
    let mut settled = Uint128::zero();
    let mut returned = Uint128::zero();
    let mut response = Response::new().add_attribute("action", "cancel_members");
    let members = batch(deps.as_ref(), &addresses)?;
    let mut cancelled: u64 = 0;
    for address in &members {
        let mut m = MEMBERS
            .may_load(deps.storage, address)?
            .ok_or_else(|| ContractError::MemberNotFound(address.to_string()))?;
        // Nothing to reclaim from a member paid out or cancelled already, so
        // neither a last claim nor an overlapping proposal fails the batch.
        if m.cancelled || m.remaining.is_zero() {
            response = response.add_event(
                Event::new("ark-grant/member_skipped").add_attribute("address", address.as_str()),
            );
            continue;
        }
        cancelled += 1;
        let until = suspension(deps.storage, &m)?.map_or(now, |s| s.at);
        let (elapsed, _) = rules::elapsed(&m.schedule, m.start, until);
        let pay = rules::accrued(m.amount, &m.schedule, m.paid, elapsed)?;
        if !pay.is_zero() {
            m.released = m.released.checked_add(pay)?;
            m.remaining = m.remaining.checked_sub(pay)?;
            m.paid = elapsed;
            response = response.add_message(send(&config, address, pay));
        }
        let rest = m.remaining;
        m.remaining = Uint128::zero();
        m.cancelled = true;
        MEMBERS.save(deps.storage, address, &m)?;
        settled = settled.checked_add(pay)?;
        returned = returned.checked_add(rest)?;
        response = response.add_event(
            Event::new("ark-grant/member_cancelled")
                .add_attribute("address", address.as_str())
                .add_attribute("settled", pay)
                .add_attribute("returned", rest),
        );
    }
    settle_members(deps.storage, settled)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_sub(returned)?)
    })?;
    MEMBERS_CANCELLED.update(deps.storage, |n| -> StdResult<_> { Ok(n + cancelled) })?;
    Ok(response)
}

fn set_release_address(
    deps: DepsMut,
    info: MessageInfo,
    id: u64,
    address: String,
) -> Result<Response, ContractError> {
    let mut grant = GRANTS
        .may_load(deps.storage, id)?
        .ok_or(ContractError::GrantNotFound(id))?;
    let person = PERSONS.load(deps.storage, &grant.grantee)?;
    if person.controller != info.sender {
        return Err(ContractError::Unauthorized("not the controller".into()));
    }
    // A cancelled stream still holds what had elapsed for its payee, so the
    // controller may point that anywhere it can be paid.
    if grant.cancelled && matches!(grant.kind, Kind::Ownership { .. }) {
        return Err(ContractError::Cancelled(id));
    }
    if grant.remaining.is_zero() {
        return Err(ContractError::Exhausted(id));
    }
    let address = deps.api.addr_validate(&address)?;
    match &mut grant.kind {
        Kind::Ownership {
            release_address, ..
        } => *release_address = Some(address.clone()),
        Kind::Stream { payee, .. } => *payee = address.clone(),
    }
    GRANTS.save(deps.storage, id, &grant)?;
    Ok(Response::new().add_event(
        Event::new("ark-grant/release_address_set")
            .add_attribute("id", id.to_string())
            .add_attribute("address", address.as_str()),
    ))
}

fn set_controller(
    deps: DepsMut,
    grantee: Addr,
    mut person: Person,
    controller: String,
) -> Result<Response, ContractError> {
    let controller = deps.api.addr_validate(&controller)?;
    person.controller = controller.clone();
    PERSONS.save(deps.storage, &grantee, &person)?;
    Ok(Response::new().add_event(
        Event::new("ark-grant/controller_set")
            .add_attribute("grantee", grantee.as_str())
            .add_attribute("controller", controller.as_str()),
    ))
}

fn add_grant(
    mut deps: DepsMut,
    env: Env,
    grantee: String,
    amount: Uint128,
    schedule: Vec<crate::msg::Period>,
    seat_holder: bool,
    release_address: Option<String>,
) -> Result<Response, ContractError> {
    if amount.is_zero() {
        return Err(ContractError::ZeroAmount);
    }
    rules::validate_schedule(&schedule)?;
    let config = CONFIG.load(deps.storage)?;
    // Every tranche must give each period a coin; the smallest decides.
    rules::split(rules::smallest_tranche(amount, config.cap.unit)?, &schedule)?;
    // Grant and gas against free, not unallocated: a grant arrives with its
    // own spend, so promised gas is no floor a proposal can trip on.
    let gas = rules::fee_reserve(amount, config.cap.unit, config.fee_allowance)?;
    let needed = amount.checked_add(gas)?;
    let available = free(deps.as_ref(), &env, &config)?;
    if needed > available {
        return Err(ContractError::Unallocated { available, needed });
    }
    let grantee = deps.api.addr_validate(&grantee)?;
    let known = PERSONS.may_load(deps.storage, &grantee)?;
    // The first tranche goes where the proposal says, else to the grantee
    // while the contract has not paid them: an address holding an account
    // cannot take a vesting account, so a repeat grant waits for the
    // controller to name one.
    let release_address = match release_address {
        Some(a) => Some(deps.api.addr_validate(&a)?),
        None if known.is_none() => Some(grantee.clone()),
        None => None,
    };

    // Governance restates whether the person holds a seat on each grant; a
    // first grant makes the grantee address the controller.
    let person = match known {
        Some(mut p) => {
            p.seat_holder = seat_holder;
            p
        }
        None => Person {
            controller: grantee.clone(),
            seat_holder,
            released: Uint128::zero(),
        },
    };
    PERSONS.save(deps.storage, &grantee, &person)?;

    let id = NEXT_ID.load(deps.storage)?;
    NEXT_ID.save(deps.storage, &(id + 1))?;
    let grant = Grant {
        id,
        grantee: grantee.clone(),
        amount,
        released: Uint128::zero(),
        remaining: amount,
        schedule,
        cancelled: false,
        kind: Kind::Ownership {
            fee_reserve: gas,
            release_address: release_address.clone(),
        },
    };
    GRANTS.save(deps.storage, id, &grant)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_add(amount)?)
    })?;
    FEES_RESERVED.update(deps.storage, |r| -> Result<_, ContractError> {
        Ok(r.checked_add(gas)?)
    })?;

    let mut response = Response::new().add_event(
        Event::new("ark-grant/added")
            .add_attribute("id", id.to_string())
            .add_attribute("grantee", grantee.as_str())
            .add_attribute("amount", amount)
            .add_attribute("gas", gas)
            .add_attribute("seat_holder", seat_holder.to_string()),
    );
    // The first tranche pays now if there is an address and the rules allow
    // one; a grant with no room yet simply waits in escrow.
    if let Some(address) = &release_address {
        let r = releasable(deps.as_ref(), &env, &config, &grant, &person)?;
        if !r.amount.is_zero() {
            let (sub, event) = tranche(&mut deps, &env, &config, &grant, address, r.amount)?;
            response = response.add_submessage(sub).add_event(event);
        }
    }
    Ok(response)
}

fn add_stream(
    deps: DepsMut,
    env: Env,
    grantee: String,
    amount: Uint128,
    schedule: Vec<crate::msg::Period>,
) -> Result<Response, ContractError> {
    if amount.is_zero() {
        return Err(ContractError::ZeroAmount);
    }
    rules::validate_schedule(&schedule)?;
    // Every period must carry a coin: a send of nothing fails.
    rules::split(amount, &schedule)?;
    let config = CONFIG.load(deps.storage)?;
    let available = free(deps.as_ref(), &env, &config)?;
    if amount > available {
        return Err(ContractError::Unallocated {
            available,
            needed: amount,
        });
    }
    let grantee = deps.api.addr_validate(&grantee)?;
    // A first grant makes the grantee address the controller. A stream says
    // nothing about the seat; the next ownership grant restates it.
    if !PERSONS.has(deps.storage, &grantee) {
        PERSONS.save(
            deps.storage,
            &grantee,
            &Person {
                controller: grantee.clone(),
                seat_holder: false,
                released: Uint128::zero(),
            },
        )?;
    }
    let id = NEXT_ID.load(deps.storage)?;
    NEXT_ID.save(deps.storage, &(id + 1))?;
    let start = env.block.time.seconds();
    let periods = schedule.len();
    let grant = Grant {
        id,
        grantee: grantee.clone(),
        amount,
        released: Uint128::zero(),
        remaining: amount,
        schedule,
        cancelled: false,
        kind: Kind::Stream {
            start,
            paid: 0,
            payee: grantee.clone(),
        },
    };
    GRANTS.save(deps.storage, id, &grant)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_add(amount)?)
    })?;
    Ok(Response::new().add_event(
        Event::new("ark-grant/stream_added")
            .add_attribute("id", id.to_string())
            .add_attribute("grantee", grantee.as_str())
            .add_attribute("amount", amount)
            .add_attribute("periods", periods.to_string())
            .add_attribute("start", start.to_string()),
    ))
}

fn release(mut deps: DepsMut, env: Env, id: u64) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    let mut grant = GRANTS
        .may_load(deps.storage, id)?
        .ok_or(ContractError::GrantNotFound(id))?;
    // A cancelled stream still holds what had elapsed by the vote for its
    // payee; nothing else cancelled pays.
    let held =
        grant.cancelled && matches!(grant.kind, Kind::Stream { .. }) && !grant.remaining.is_zero();
    if grant.cancelled && !held {
        return Err(ContractError::Cancelled(id));
    }
    if grant.remaining.is_zero() {
        return Err(ContractError::Exhausted(id));
    }
    match grant.kind.clone() {
        Kind::Ownership {
            release_address, ..
        } => {
            let address = release_address.ok_or(ContractError::NoReleaseAddress(id))?;
            let person = PERSONS.load(deps.storage, &grant.grantee)?;
            let r = releasable(deps.as_ref(), &env, &config, &grant, &person)?;
            if r.amount.is_zero() {
                return Err(ContractError::NothingReleasable(id));
            }
            let (sub, event) = tranche(&mut deps, &env, &config, &grant, &address, r.amount)?;
            Ok(Response::new().add_submessage(sub).add_event(event))
        }
        Kind::Stream { start, paid, payee } => {
            let (elapsed, amount) = if held {
                (paid, grant.remaining)
            } else {
                let (elapsed, _) = rules::elapsed(&grant.schedule, start, env.block.time.seconds());
                let amount = rules::accrued(grant.amount, &grant.schedule, paid, elapsed)?;
                (elapsed, amount)
            };
            if amount.is_zero() {
                return Err(ContractError::NothingReleasable(id));
            }
            settle_stream(deps.storage, &mut grant, elapsed, amount)?;
            GRANTS.save(deps.storage, id, &grant)?;
            Ok(Response::new()
                .add_message(send(&config, &payee, amount))
                .add_event(
                    Event::new("ark-grant/paid")
                        .add_attribute("id", id.to_string())
                        .add_attribute("address", payee.as_str())
                        .add_attribute("amount", amount)
                        .add_attribute("periods", (elapsed - paid).to_string())
                        .add_attribute("remaining", grant.remaining),
                ))
        }
    }
}

/// settle_stream moves a stream's figures for the periods through elapsed:
/// here rather than in a reply, since a send within the balance cannot fail
/// and escrow holds the balance. The caller saves the grant.
fn settle_stream(
    storage: &mut dyn Storage,
    grant: &mut Grant,
    elapsed: u32,
    amount: Uint128,
) -> Result<(), ContractError> {
    grant.released = grant.released.checked_add(amount)?;
    grant.remaining = grant.remaining.checked_sub(amount)?;
    if let Kind::Stream { paid, .. } = &mut grant.kind {
        *paid = elapsed;
    }
    ESCROWED.update(storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_sub(amount)?)
    })?;
    CONTRIBUTORS_PAID.update(storage, |t| -> Result<_, ContractError> {
        Ok(t.checked_add(amount)?)
    })?;
    Ok(())
}

/// send is a bank send from the contract: a stream's pay, spendable on
/// arrival at any address.
fn send(config: &Config, to: &Addr, amount: Uint128) -> CosmosMsg {
    CosmosMsg::Bank(BankMsg::Send {
        to_address: to.to_string(),
        amount: coins(amount.u128(), &config.denom),
    })
}

/// promise_allowance grants amount of gas to an account the contract created
/// and counts the promise, none at zero. The contract never sees the grant
/// drawn, so the count only grows.
fn promise_allowance(
    storage: &mut dyn Storage,
    config: &Config,
    env: &Env,
    address: &Addr,
    amount: Uint128,
) -> Result<Option<CosmosMsg>, ContractError> {
    if amount.is_zero() {
        return Ok(None);
    }
    FEES_PROMISED.update(storage, |p| -> Result<_, ContractError> {
        Ok(p.checked_add(amount)?)
    })?;
    Ok(Some(proto::grant_fee_allowance(
        env.contract.address.as_str(),
        address.as_str(),
        &config.denom,
        amount,
    )))
}

/// tranche dispatches one vesting account creation; the reply settles the
/// grant's figures, so nothing here is written until the account exists.
fn tranche(
    deps: &mut DepsMut,
    env: &Env,
    config: &Config,
    grant: &Grant,
    address: &Addr,
    amount: Uint128,
) -> Result<(SubMsg, Event), ContractError> {
    let amounts = rules::split(amount, &grant.schedule)?;
    let msg = proto::create_vesting_account(
        env.contract.address.as_str(),
        address.as_str(),
        &config.denom,
        env.block.time.seconds() as i64,
        &grant.schedule,
        &amounts,
    );
    let id = next_reply_id(deps)?;
    let payload = to_json_binary(&Pending::Tranche {
        id: grant.id,
        address: address.clone(),
        amount,
    })?;
    let event = Event::new("ark-grant/tranche")
        .add_attribute("id", grant.id.to_string())
        .add_attribute("address", address.as_str())
        .add_attribute("amount", amount);
    Ok((SubMsg::reply_always(msg, id).with_payload(payload), event))
}

fn cancel_grant(deps: DepsMut, env: Env, id: u64) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    let mut grant = GRANTS
        .may_load(deps.storage, id)?
        .ok_or(ContractError::GrantNotFound(id))?;
    if grant.cancelled {
        return Err(ContractError::Cancelled(id));
    }
    let mut response = Response::new();
    let mut gas = Uint128::zero();
    let mut held = Uint128::zero();
    match grant.kind.clone() {
        Kind::Ownership { fee_reserve, .. } => {
            gas = fee_reserve;
            grant.kind = Kind::Ownership {
                fee_reserve: Uint128::zero(),
                release_address: None,
            };
        }
        Kind::Stream { start, paid, payee } => {
            // What has elapsed is the payee's, held for release rather than
            // sent here, so no payee can fail the cancel; the rest returns.
            let (elapsed, _) = rules::elapsed(&grant.schedule, start, env.block.time.seconds());
            held = rules::accrued(grant.amount, &grant.schedule, paid, elapsed)?;
            grant.kind = Kind::Stream {
                start,
                paid: elapsed,
                payee,
            };
        }
    }
    let returned = grant.remaining.checked_sub(held)?;
    grant.remaining = held;
    grant.cancelled = true;
    GRANTS.save(deps.storage, id, &grant)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_sub(returned)?)
    })?;
    FEES_RESERVED.update(deps.storage, |r| -> Result<_, ContractError> {
        Ok(r.checked_sub(gas)?)
    })?;
    let total = returned.checked_add(gas)?;
    response = response.add_event(
        Event::new("ark-grant/cancelled")
            .add_attribute("id", id.to_string())
            .add_attribute("returned", returned)
            .add_attribute("gas_returned", gas)
            .add_attribute("held", held),
    );
    if !total.is_zero() {
        response = response.add_message(proto::fund_community_pool(
            env.contract.address.as_str(),
            &config.denom,
            total,
        ));
    }
    Ok(response)
}

/// releasable is what release would pay a grant now: for ownership, by the
/// cap and, for a seat holder, the bloc rule against bonded stake read now;
/// for a stream, by the clock.
fn releasable(
    deps: Deps,
    env: &Env,
    config: &Config,
    grant: &Grant,
    person: &Person,
) -> Result<ReleasableResponse, ContractError> {
    let idle = grant.cancelled || grant.remaining.is_zero();
    match &grant.kind {
        Kind::Ownership { .. } => {
            let bonded = proto::bonded_tokens(&deps.querier)?;
            let own = own(config, person)?;
            let seat = own.checked_sub(person.released)?;
            let cap = rules::cap_total(bonded, own, seat, &config.cap)?;
            let mut allowance = cap.saturating_sub(own);
            let mut seat_allowance = None;
            if person.seat_holder {
                let (seat_grants, seat_remaining) = seat_totals(deps)?;
                let pool = rules::seat_allowance(bonded, config.founding_stake, seat_grants);
                let share = rules::pro_rata(pool, grant.remaining, seat_remaining);
                allowance = allowance.min(share);
                seat_allowance = Some(share);
            }
            let amount = if idle {
                Uint128::zero()
            } else {
                rules::release_amount(grant.remaining, allowance, config.cap.unit)
            };
            Ok(ReleasableResponse {
                id: grant.id,
                amount,
                rule: Rule::Cap {
                    bonded,
                    own,
                    cap,
                    seat_allowance,
                },
            })
        }
        Kind::Stream { start, paid, .. } => {
            // Cancelled, the clock has stopped and what it holds is the payee's.
            let (elapsed, next_at) = if grant.cancelled {
                (*paid, None)
            } else {
                rules::elapsed(&grant.schedule, *start, env.block.time.seconds())
            };
            let amount = if grant.cancelled {
                grant.remaining
            } else {
                rules::accrued(grant.amount, &grant.schedule, *paid, elapsed)?
            };
            Ok(ReleasableResponse {
                id: grant.id,
                amount,
                rule: Rule::Clock {
                    start: *start,
                    elapsed,
                    paid: *paid,
                    next_at,
                },
            })
        }
    }
}

/// seat_totals sums what this contract has released to seat holders and
/// what their ownership grants still hold in escrow; streams are pay and
/// stay outside.
fn seat_totals(deps: Deps) -> Result<(Uint128, Uint128), ContractError> {
    let mut received = Uint128::zero();
    for item in PERSONS.range(deps.storage, None, None, Order::Ascending) {
        let (_, p) = item?;
        if p.seat_holder {
            received = received.checked_add(p.released)?;
        }
    }
    let mut remaining = Uint128::zero();
    for item in GRANTS.range(deps.storage, None, None, Order::Ascending) {
        let (_, g) = item?;
        if g.cancelled || g.remaining.is_zero() || matches!(g.kind, Kind::Stream { .. }) {
            continue;
        }
        if PERSONS.load(deps.storage, &g.grantee)?.seat_holder {
            remaining = remaining.checked_add(g.remaining)?;
        }
    }
    Ok((received, remaining))
}
