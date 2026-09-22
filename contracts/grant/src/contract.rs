use cosmwasm_std::{
    entry_point, from_json, to_json_binary, Addr, Binary, Deps, DepsMut, Env, Event, MessageInfo,
    Order, Reply, Response, StdResult, SubMsg, SubMsgResult, Uint128,
};
use cw2::set_contract_version;

use crate::error::ContractError;
use crate::msg::{
    ConfigResponse, ExecuteMsg, GrantResponse, InstantiateMsg, IssuanceResponse, MemberResponse,
    MemberStatus, MigrateMsg, PersonResponse, QueryMsg, ReleasableResponse, SudoMsg,
    TotalsResponse,
};
use crate::proto;
use crate::rules;
use crate::state::{
    Config, Grant, IssuanceWindow, Pending, Person, CONFIG, CONTRIBUTORS_PAID, ESCROWED, GRANTS,
    ISSUANCE, MEMBERS, MEMBERS_ISSUED, MEMBERS_REJECTED, NEXT_ID, PERSONS, REPLY_SEQ,
};

const CONTRACT_NAME: &str = "crates.io:ark-grant";
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

/// MAX_BATCH bounds one registration call.
pub const MAX_BATCH: usize = 100;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    env: Env,
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
    ISSUANCE.save(
        deps.storage,
        &IssuanceWindow {
            start: env.block.time.seconds(),
            issued: 0,
        },
    )?;
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
    CONTRIBUTORS_PAID.save(deps.storage, &Uint128::zero())?;
    MEMBERS_ISSUED.save(deps.storage, &0)?;
    MEMBERS_REJECTED.save(deps.storage, &0)?;
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
        SudoMsg::SetMemberGrant {
            amount,
            schedule,
            fee_allowance,
        } => {
            if amount.is_zero() {
                return Err(ContractError::ZeroAmount);
            }
            rules::validate_schedule(&schedule)?;
            rules::split(amount, &schedule)?;
            CONFIG.update(deps.storage, |mut c| -> StdResult<_> {
                c.member_grant = amount;
                c.member_schedule = schedule;
                c.fee_allowance = fee_allowance;
                Ok(c)
            })?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/member_grant_set").add_attribute("amount", amount),
            ))
        }
        SudoMsg::AddGrant {
            grantee,
            amount,
            schedule,
            seat_holder,
        } => add_grant(deps, env, grantee, amount, schedule, seat_holder),
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
        (Pending::Member { address }, SubMsgResult::Ok(_)) => {
            MEMBERS.save(
                deps.storage,
                &address,
                &MemberStatus::Issued {
                    height: env.block.height,
                },
            )?;
            MEMBERS_ISSUED.update(deps.storage, |n| -> StdResult<_> { Ok(n + 1) })?;
            let mut response = Response::new().add_event(
                Event::new("ark-grant/member_issued").add_attribute("address", address.as_str()),
            );
            if !config.fee_allowance.is_zero() {
                response = response.add_message(proto::grant_fee_allowance(
                    env.contract.address.as_str(),
                    address.as_str(),
                    &config.denom,
                    config.fee_allowance,
                ));
            }
            Ok(response)
        }
        (Pending::Member { address }, SubMsgResult::Err(error)) => {
            MEMBERS.save(
                deps.storage,
                &address,
                &MemberStatus::Rejected {
                    height: env.block.height,
                    error: error.clone(),
                },
            )?;
            MEMBERS_REJECTED.update(deps.storage, |n| -> StdResult<_> { Ok(n + 1) })?;
            Ok(Response::new().add_event(
                Event::new("ark-grant/member_rejected")
                    .add_attribute("address", address.as_str())
                    .add_attribute("error", error),
            ))
        }
        (
            Pending::Tranche {
                id,
                address,
                amount,
            },
            SubMsgResult::Ok(_),
        ) => {
            let mut grant = GRANTS.load(deps.storage, id)?;
            grant.released = grant.released.checked_add(amount)?;
            grant.remaining = grant.remaining.checked_sub(amount)?;
            grant.release_address = None;
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
            Ok(Response::new().add_event(
                Event::new("ark-grant/released")
                    .add_attribute("id", id.to_string())
                    .add_attribute("address", address.as_str())
                    .add_attribute("amount", amount)
                    .add_attribute("remaining", grant.remaining),
            ))
        }
        (Pending::Tranche { id, address, .. }, SubMsgResult::Err(error)) => {
            GRANTS.update(deps.storage, id, |g| -> Result<_, ContractError> {
                let mut g = g.ok_or(ContractError::GrantNotFound(id))?;
                g.release_address = None;
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
                own: own(&config, &p),
            })
        }
        QueryMsg::Releasable { id } => {
            let config = CONFIG.load(deps.storage)?;
            let grant = GRANTS.load(deps.storage, id)?;
            let person = PERSONS.load(deps.storage, &grant.grantee)?;
            let r = releasable(deps, &config, &grant, &person)
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
            to_json_binary(&TotalsResponse {
                balance,
                unallocated: balance.saturating_sub(escrowed),
                escrowed,
                contributors_paid: CONTRIBUTORS_PAID.load(deps.storage)?,
                members_issued: MEMBERS_ISSUED.load(deps.storage)?,
                members_rejected: MEMBERS_REJECTED.load(deps.storage)?,
            })
        }
        QueryMsg::Member { address } => {
            let address = deps.api.addr_validate(&address)?;
            to_json_binary(&MemberResponse {
                status: MEMBERS.may_load(deps.storage, &address)?,
            })
        }
        QueryMsg::Issuance {} => {
            let config = CONFIG.load(deps.storage)?;
            let window = current_window(deps.storage, &env, &config)?;
            to_json_binary(&IssuanceResponse {
                window_start: window.start,
                window_ends: window
                    .start
                    .saturating_add(config.issuance_limit.window_seconds),
                issued: window.issued,
                remaining: config
                    .issuance_limit
                    .max_members
                    .saturating_sub(window.issued),
            })
        }
    }
}

/// current_window is the issuance window as of now: the stored one, or a
/// fresh one if it has elapsed. Nothing is written; the caller decides.
fn current_window(
    storage: &dyn cosmwasm_std::Storage,
    env: &Env,
    config: &Config,
) -> StdResult<IssuanceWindow> {
    let window = ISSUANCE.load(storage)?;
    let now = env.block.time.seconds();
    if now
        >= window
            .start
            .saturating_add(config.issuance_limit.window_seconds)
    {
        return Ok(IssuanceWindow {
            start: now,
            issued: 0,
        });
    }
    Ok(window)
}

fn grant_response(g: Grant) -> GrantResponse {
    GrantResponse {
        id: g.id,
        grantee: g.grantee,
        amount: g.amount,
        released: g.released,
        remaining: g.remaining,
        schedule: g.schedule,
        release_address: g.release_address,
        cancelled: g.cancelled,
    }
}

/// unallocated is the balance not yet promised to a grant.
fn unallocated(deps: Deps, env: &Env, config: &Config) -> Result<Uint128, ContractError> {
    let balance = deps
        .querier
        .query_balance(env.contract.address.clone(), &config.denom)?
        .amount;
    Ok(balance.saturating_sub(ESCROWED.load(deps.storage)?))
}

/// own is everything counted against a person: the seat if they hold one
/// and what this contract has released to them, all assumed bonded.
fn own(config: &Config, person: &Person) -> Uint128 {
    let seat = if person.seat_holder {
        config.seat_stake
    } else {
        Uint128::zero()
    };
    seat + person.released
}

fn next_reply_id(deps: &mut DepsMut) -> StdResult<u64> {
    REPLY_SEQ.update(deps.storage, |n| -> StdResult<_> { Ok(n + 1) })
}

fn register_members(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    addresses: Vec<String>,
) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    match &config.registrar {
        Some(r) if *r == info.sender => {}
        _ => return Err(ContractError::Unauthorized("not the registrar".into())),
    }
    if addresses.is_empty() {
        return Err(ContractError::EmptyBatch);
    }
    if addresses.len() > MAX_BATCH {
        return Err(ContractError::BatchTooLarge(addresses.len(), MAX_BATCH));
    }
    // Addresses first, so a bad batch is reported as such before its cost.
    let mut members = Vec::with_capacity(addresses.len());
    for raw in &addresses {
        let address = deps.api.addr_validate(raw)?;
        if MEMBERS.has(deps.storage, &address) || members.contains(&address) {
            return Err(ContractError::MemberExists(address.to_string()));
        }
        members.push(address);
    }
    // The window bounds what a registrar key can issue before governance can
    // replace it; attempted registrations count, whatever their outcome.
    let mut window = current_window(deps.storage, &env, &config)?;
    let requested = members.len() as u64;
    if window.issued.saturating_add(requested) > config.issuance_limit.max_members {
        return Err(ContractError::IssuanceLimit {
            remaining: config
                .issuance_limit
                .max_members
                .saturating_sub(window.issued),
            requested,
            window_ends: window
                .start
                .saturating_add(config.issuance_limit.window_seconds),
        });
    }
    window.issued = window.issued.saturating_add(requested);
    ISSUANCE.save(deps.storage, &window)?;
    let needed = config
        .member_grant
        .checked_mul(Uint128::new(members.len() as u128))?;
    let available = unallocated(deps.as_ref(), &env, &config)?;
    if needed > available {
        return Err(ContractError::Unallocated { available, needed });
    }

    let amounts = rules::split(config.member_grant, &config.member_schedule)?;
    let start = env.block.time.seconds() as i64;
    let mut deps = deps;
    let mut response = Response::new().add_attribute("action", "register_members");
    for address in members {
        // Recorded now so nothing between dispatch and reply can register it
        // again; the reply overwrites it with the outcome.
        MEMBERS.save(
            deps.storage,
            &address,
            &MemberStatus::Rejected {
                height: env.block.height,
                error: "pending".into(),
            },
        )?;
        let msg = proto::create_vesting_account(
            env.contract.address.as_str(),
            address.as_str(),
            &config.denom,
            start,
            &config.member_schedule,
            &amounts,
        );
        let id = next_reply_id(&mut deps)?;
        let payload = to_json_binary(&Pending::Member { address })?;
        response = response.add_submessage(SubMsg::reply_always(msg, id).with_payload(payload));
    }
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
    if grant.cancelled {
        return Err(ContractError::Cancelled(id));
    }
    if grant.remaining.is_zero() {
        return Err(ContractError::Exhausted(id));
    }
    let address = deps.api.addr_validate(&address)?;
    grant.release_address = Some(address.clone());
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
) -> Result<Response, ContractError> {
    if amount.is_zero() {
        return Err(ContractError::ZeroAmount);
    }
    rules::validate_schedule(&schedule)?;
    let config = CONFIG.load(deps.storage)?;
    let available = unallocated(deps.as_ref(), &env, &config)?;
    if amount > available {
        return Err(ContractError::Unallocated {
            available,
            needed: amount,
        });
    }
    let grantee = deps.api.addr_validate(&grantee)?;

    // Governance restates whether the person holds a seat on each grant; a
    // first grant makes the grantee address the controller.
    let person = match PERSONS.may_load(deps.storage, &grantee)? {
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
        release_address: Some(grantee.clone()),
        cancelled: false,
    };
    GRANTS.save(deps.storage, id, &grant)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_add(amount)?)
    })?;

    let mut response = Response::new().add_event(
        Event::new("ark-grant/added")
            .add_attribute("id", id.to_string())
            .add_attribute("grantee", grantee.as_str())
            .add_attribute("amount", amount)
            .add_attribute("seat_holder", seat_holder.to_string()),
    );
    // The first tranche pays now if the rules allow one; a grant with no
    // room yet simply waits in escrow.
    let r = releasable(deps.as_ref(), &config, &grant, &person)?;
    if !r.amount.is_zero() {
        let (sub, event) = tranche(&mut deps, &env, &config, &grant, &grantee, r.amount)?;
        response = response.add_submessage(sub).add_event(event);
    }
    Ok(response)
}

fn release(mut deps: DepsMut, env: Env, id: u64) -> Result<Response, ContractError> {
    let config = CONFIG.load(deps.storage)?;
    let grant = GRANTS
        .may_load(deps.storage, id)?
        .ok_or(ContractError::GrantNotFound(id))?;
    if grant.cancelled {
        return Err(ContractError::Cancelled(id));
    }
    if grant.remaining.is_zero() {
        return Err(ContractError::Exhausted(id));
    }
    let address = grant
        .release_address
        .clone()
        .ok_or(ContractError::NoReleaseAddress(id))?;
    let person = PERSONS.load(deps.storage, &grant.grantee)?;
    let r = releasable(deps.as_ref(), &config, &grant, &person)?;
    if r.amount.is_zero() {
        return Err(ContractError::NothingReleasable(id));
    }
    let (sub, event) = tranche(&mut deps, &env, &config, &grant, &address, r.amount)?;
    Ok(Response::new().add_submessage(sub).add_event(event))
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
    let returned = grant.remaining;
    grant.remaining = Uint128::zero();
    grant.release_address = None;
    grant.cancelled = true;
    GRANTS.save(deps.storage, id, &grant)?;
    ESCROWED.update(deps.storage, |e| -> Result<_, ContractError> {
        Ok(e.checked_sub(returned)?)
    })?;
    let mut response = Response::new().add_event(
        Event::new("ark-grant/cancelled")
            .add_attribute("id", id.to_string())
            .add_attribute("returned", returned),
    );
    if !returned.is_zero() {
        response = response.add_message(proto::fund_community_pool(
            env.contract.address.as_str(),
            &config.denom,
            returned,
        ));
    }
    Ok(response)
}

/// releasable applies the cap and, for a seat holder, the bloc rule to one
/// grant against bonded stake read now.
fn releasable(
    deps: Deps,
    config: &Config,
    grant: &Grant,
    person: &Person,
) -> Result<ReleasableResponse, ContractError> {
    let bonded = proto::bonded_tokens(&deps.querier)?;
    let own = own(config, person);
    let cap = rules::cap_total(bonded, own, &config.cap);
    let mut allowance = cap.saturating_sub(own);
    let mut seat_share = None;
    if person.seat_holder {
        let (seat_grants, seat_remaining) = seat_totals(deps)?;
        let pool = rules::seat_allowance(bonded, config.founding_stake, seat_grants);
        let share = rules::pro_rata(pool, grant.remaining, seat_remaining);
        allowance = allowance.min(share);
        seat_share = Some(share);
    }
    let amount = if grant.cancelled || grant.remaining.is_zero() {
        Uint128::zero()
    } else {
        rules::release_amount(grant.remaining, allowance, config.cap.unit)
    };
    Ok(ReleasableResponse {
        id: grant.id,
        amount,
        bonded,
        own,
        cap,
        seat_allowance: seat_share,
    })
}

/// seat_totals sums what this contract has released to seat holders and
/// what their grants still hold in escrow.
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
        if g.cancelled || g.remaining.is_zero() {
            continue;
        }
        if PERSONS.load(deps.storage, &g.grantee)?.seat_holder {
            remaining = remaining.checked_add(g.remaining)?;
        }
    }
    Ok((received, remaining))
}
