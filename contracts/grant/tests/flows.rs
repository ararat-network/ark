//! Flow tests on cw-multi-test with a stub for the chain: the stub creates
//! vesting accounts by moving the coins, refuses an address that already
//! holds an account, records fee allowances, and answers the pool query with
//! whatever bonded stake the test sets.

use anyhow::bail;
use ark_grant::contract::{execute, instantiate, migrate, query, reply, sudo, MAX_BATCH};
use ark_grant::msg::{
    CapRule, ConfigResponse, ExecuteMsg, GrantResponse, InstantiateMsg, IssuanceEntry,
    IssuanceLimit, IssuanceResponse, Kind, Member, MemberResponse, Period, PersonResponse,
    QueryMsg, ReleasableResponse, Rule, SudoMsg, Suspension, TotalsResponse,
};
use ark_grant::proto::{encode_pool_response, POOL_QUERY_PATH};
use ark_grant::ContractError;
use cosmos_sdk_proto::cosmos::distribution::v1beta1::MsgFundCommunityPool;
use cosmos_sdk_proto::cosmos::feegrant::v1beta1::{BasicAllowance, MsgGrantAllowance};
use cosmos_sdk_proto::cosmos::vesting::v1beta1::MsgCreatePeriodicVestingAccount;
use cosmwasm_std::{
    coins, Addr, AnyMsg, Api, BankMsg, Binary, BlockInfo, CosmosMsg, CustomMsg, CustomQuery, Empty,
    GrpcQuery, Querier, Storage, Uint128,
};
use cw_multi_test::error::AnyResult;
use cw_multi_test::{
    App, AppBuilder, AppResponse, BankKeeper, ContractWrapper, CosmosRouter, DistributionKeeper,
    Executor, FailingModule, GovFailingModule, IbcFailingModule, MockApiBech32, StakeKeeper,
    Stargate, WasmKeeper,
};
use prost::Message;
use serde::de::DeserializeOwned;

const DENOM: &str = "anoah";
const NOAH: u128 = 1_000_000_000_000_000_000;

fn noah(n: u128) -> Uint128 {
    Uint128::new(n * NOAH)
}

fn standard() -> Vec<Period> {
    let mut s = vec![Period {
        length: 31_536_000,
        parts: 12,
    }];
    s.extend((0..36).map(|_| Period {
        length: 2_628_000,
        parts: 1,
    }));
    s
}

/// member is the plan's member schedule: a tenth after a second, then twelve
/// months of three fortieths.
fn member() -> Vec<Period> {
    let mut s = vec![Period {
        length: 1,
        parts: 4,
    }];
    s.extend((0..12).map(|_| Period {
        length: 2_628_000,
        parts: 3,
    }));
    s
}

// The stub keeps its facts in the app's storage under its own prefix.
const BONDED_KEY: &[u8] = b"stub/bonded";

fn account_key(addr: &str) -> Vec<u8> {
    [b"stub/account/", addr.as_bytes()].concat()
}

fn allowance_key(addr: &str) -> Vec<u8> {
    [b"stub/allowance/", addr.as_bytes()].concat()
}

struct ChainStub {
    pool: Addr,
}

impl Stargate for ChainStub {
    fn execute_any<ExecC, QueryC>(
        &self,
        api: &dyn Api,
        storage: &mut dyn Storage,
        router: &dyn CosmosRouter<ExecC = ExecC, QueryC = QueryC>,
        block: &BlockInfo,
        sender: Addr,
        msg: AnyMsg,
    ) -> AnyResult<AppResponse>
    where
        ExecC: CustomMsg + DeserializeOwned + 'static,
        QueryC: CustomQuery + DeserializeOwned + 'static,
    {
        match msg.type_url.as_str() {
            "/cosmos.vesting.v1beta1.MsgCreatePeriodicVestingAccount" => {
                let m = MsgCreatePeriodicVestingAccount::decode(msg.value.as_slice())?;
                if m.from_address != sender.as_str() {
                    bail!("signer {} is not the sender {}", m.from_address, sender);
                }
                let key = account_key(&m.to_address);
                if storage.get(&key).is_some() {
                    bail!("account {} already exists", m.to_address);
                }
                let total: u128 = m
                    .vesting_periods
                    .iter()
                    .flat_map(|p| p.amount.iter())
                    .map(|c| c.amount.parse::<u128>().unwrap())
                    .sum();
                let record = format!("{}:{}:{}", m.start_time, m.vesting_periods.len(), total);
                storage.set(&key, record.as_bytes());
                let send = CosmosMsg::<ExecC>::Bank(BankMsg::Send {
                    to_address: m.to_address,
                    amount: coins(total, DENOM),
                });
                router.execute(api, storage, block, sender, send)
            }
            "/cosmos.feegrant.v1beta1.MsgGrantAllowance" => {
                let m = MsgGrantAllowance::decode(msg.value.as_slice())?;
                let allowance = m.allowance.expect("allowance");
                assert_eq!(
                    allowance.type_url,
                    "/cosmos.feegrant.v1beta1.BasicAllowance"
                );
                let basic = BasicAllowance::decode(allowance.value.as_slice())?;
                storage.set(
                    &allowance_key(&m.grantee),
                    basic.spend_limit[0].amount.as_bytes(),
                );
                Ok(AppResponse::default())
            }
            "/cosmos.distribution.v1beta1.MsgFundCommunityPool" => {
                let m = MsgFundCommunityPool::decode(msg.value.as_slice())?;
                let amount: u128 = m.amount[0].amount.parse()?;
                let send = CosmosMsg::<ExecC>::Bank(BankMsg::Send {
                    to_address: self.pool.to_string(),
                    amount: coins(amount, DENOM),
                });
                router.execute(api, storage, block, sender, send)
            }
            other => bail!("unexpected message {other}"),
        }
    }

    fn query_grpc(
        &self,
        _api: &dyn Api,
        storage: &dyn Storage,
        _querier: &dyn Querier,
        _block: &BlockInfo,
        request: GrpcQuery,
    ) -> AnyResult<Binary> {
        if request.path != POOL_QUERY_PATH {
            bail!("unexpected query {}", request.path);
        }
        let bonded = storage
            .get(BONDED_KEY)
            .map(|b| String::from_utf8(b).unwrap().parse::<u128>().unwrap())
            .unwrap_or(0);
        Ok(encode_pool_response(Uint128::new(bonded), Uint128::zero())?)
    }
}

type TestApp = App<
    BankKeeper,
    MockApiBech32,
    cosmwasm_std::testing::MockStorage,
    FailingModule<Empty, Empty, Empty>,
    WasmKeeper<Empty, Empty>,
    StakeKeeper,
    DistributionKeeper,
    IbcFailingModule,
    GovFailingModule,
    ChainStub,
>;

struct Fixture {
    app: TestApp,
    contract: Addr,
    gov: Addr,
    registrar: Addr,
    pool: Addr,
}

impl Fixture {
    fn new() -> Self {
        let api = MockApiBech32::new("ark");
        let gov = api.addr_make("gov");
        let registrar = api.addr_make("registrar");
        let pool = api.addr_make("community_pool");
        let mut app = AppBuilder::new()
            .with_api(api)
            .with_stargate(ChainStub { pool: pool.clone() })
            .build(|router, _, storage| {
                router
                    .bank
                    .init_balance(storage, &gov, coins(1_000_000_000 * NOAH, DENOM))
                    .unwrap();
            });
        let code = app.store_code(Box::new(
            ContractWrapper::new(execute, instantiate, query)
                .with_sudo(sudo)
                .with_reply(reply)
                .with_migrate(migrate),
        ));
        let contract = app
            .instantiate_contract(
                code,
                gov.clone(),
                &InstantiateMsg {
                    denom: DENOM.into(),
                    registrar: Some(registrar.to_string()),
                    issuance_limit: IssuanceLimit {
                        max_members: 100,
                        window_seconds: 604_800,
                    },
                    member_grant: noah(10_000),
                    member_schedule: member(),
                    fee_allowance: noah(2),
                    founding_stake: noah(50_000_000),
                    seat_stake: noah(5_000_000),
                    cap: CapRule {
                        numerator: 1,
                        denominator: 5,
                        ceiling: noah(60_000_000),
                        unit: noah(1_000_000),
                    },
                },
                &[],
                "grant",
                None,
            )
            .unwrap();
        let mut f = Fixture {
            app,
            contract,
            gov,
            registrar,
            pool,
        };
        f.set_bonded(noah(50_000_000));
        f
    }

    /// fund is the community pool spend a proposal makes to the contract.
    fn fund(&mut self, amount: Uint128) {
        let gov = self.gov.clone();
        let contract = self.contract.clone();
        self.app
            .send_tokens(gov, contract, &coins(amount.u128(), DENOM))
            .unwrap();
    }

    fn set_bonded(&mut self, bonded: Uint128) {
        self.app
            .storage_mut()
            .set(BONDED_KEY, bonded.to_string().as_bytes());
    }

    fn mark_existing(&mut self, addr: &Addr) {
        self.app
            .storage_mut()
            .set(&account_key(addr.as_str()), b"existing");
    }

    /// account returns (start_time, periods, total) for a created account.
    fn account(&self, addr: &Addr) -> Option<(i64, usize, Uint128)> {
        let raw = self.app.storage().get(&account_key(addr.as_str()))?;
        let s = String::from_utf8(raw).unwrap();
        let mut parts = s.split(':');
        Some((
            parts.next()?.parse().ok()?,
            parts.next()?.parse().ok()?,
            Uint128::new(parts.next()?.parse().ok()?),
        ))
    }

    fn allowance(&self, addr: &Addr) -> Option<Uint128> {
        let raw = self.app.storage().get(&allowance_key(addr.as_str()))?;
        Some(Uint128::new(
            String::from_utf8(raw).unwrap().parse().unwrap(),
        ))
    }

    fn balance(&self, addr: &Addr) -> Uint128 {
        self.app.wrap().query_balance(addr, DENOM).unwrap().amount
    }

    fn addr(&self, name: &str) -> Addr {
        self.app.api().addr_make(name)
    }

    fn advance(&mut self, seconds: u64) {
        self.app.update_block(|block| {
            block.time = block.time.plus_seconds(seconds);
            block.height += 1;
        });
    }

    fn register(&mut self, addresses: &[&Addr]) -> AnyResult<AppResponse> {
        let registrar = self.registrar.clone();
        self.exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: strings(addresses),
            },
        )
    }

    fn release_members(&mut self, sender: &Addr, addresses: &[&Addr]) -> AnyResult<AppResponse> {
        self.exec(
            sender,
            ExecuteMsg::ReleaseMembers {
                addresses: strings(addresses),
            },
        )
    }

    fn member(&self, address: &Addr) -> MemberResponse {
        self.query(QueryMsg::Member {
            address: address.to_string(),
        })
    }

    fn exec(&mut self, sender: &Addr, msg: ExecuteMsg) -> AnyResult<AppResponse> {
        self.app
            .execute_contract(sender.clone(), self.contract.clone(), &msg, &[])
    }

    fn sudo(&mut self, msg: SudoMsg) -> AnyResult<AppResponse> {
        self.app.wasm_sudo(self.contract.clone(), &msg)
    }

    fn query<T: DeserializeOwned>(&self, msg: QueryMsg) -> T {
        self.app
            .wrap()
            .query_wasm_smart(self.contract.clone(), &msg)
            .unwrap()
    }

    fn totals(&self) -> TotalsResponse {
        self.query(QueryMsg::Totals {})
    }

    fn grant(&self, id: u64) -> GrantResponse {
        self.query(QueryMsg::Grant { id })
    }

    fn releasable(&self, id: u64) -> ReleasableResponse {
        self.query(QueryMsg::Releasable { id })
    }

    /// ownership is an ownership grant's gas reserve and next address.
    fn ownership(&self, id: u64) -> (Uint128, Option<Addr>) {
        match self.grant(id).kind {
            Kind::Ownership {
                fee_reserve,
                release_address,
            } => (fee_reserve, release_address),
            other => panic!("grant {id} is {other:?}"),
        }
    }

    /// cap is the cap rule's figures for an ownership grant: own, cap, and
    /// the seat allowance.
    fn cap(&self, id: u64) -> (Uint128, Uint128, Option<Uint128>) {
        match self.releasable(id).rule {
            Rule::Cap {
                own,
                cap,
                seat_allowance,
                ..
            } => (own, cap, seat_allowance),
            other => panic!("grant {id} is {other:?}"),
        }
    }
}

fn contract_error(err: &anyhow::Error) -> &ContractError {
    err.downcast_ref::<ContractError>()
        .unwrap_or_else(|| panic!("not a contract error: {err:?}"))
}

fn strings(addresses: &[&Addr]) -> Vec<String> {
    addresses.iter().map(|a| a.to_string()).collect()
}

#[test]
fn instantiate_records_config() {
    let f = Fixture::new();
    let config: ConfigResponse = f.query(QueryMsg::Config {});
    assert_eq!(config.registrar, Some(f.registrar.clone()));
    assert_eq!(config.member_grant, noah(10_000));
    assert_eq!(config.member_schedule.len(), 13);
    let totals = f.totals();
    assert_eq!(totals.balance, Uint128::zero());
    assert_eq!(totals.escrowed, Uint128::zero());
}

#[test]
fn members_are_paid_the_first_period_at_once_and_the_rest_by_the_clock() {
    let mut f = Fixture::new();
    f.fund(noah(30_000));
    let (alice, bob, dusted, stranger) = (
        f.addr("alice"),
        f.addr("bob"),
        f.addr("dusted"),
        f.addr("stranger"),
    );
    // A send to the address while the proposal was open.
    let gov = f.gov.clone();
    f.app
        .send_tokens(gov, dusted.clone(), &coins(1, DENOM))
        .unwrap();
    let t0 = f.app.block_info().time.seconds();
    let month = 2_628_000;
    let (tenth, period) = (noah(1_000), noah(750));

    f.register(&[&alice, &dusted, &bob]).unwrap();
    for who in [&alice, &bob] {
        assert_eq!(f.balance(who), tenth, "the first period pays at once");
        assert_eq!(f.account(who), None, "no vesting account");
        assert_eq!(
            f.allowance(who),
            None,
            "members pay gas from the first period"
        );
        let m = f.member(who);
        assert_eq!(
            m.member,
            Some(Member {
                start: t0,
                amount: noah(10_000),
                schedule: member(),
                released: tenth,
                remaining: noah(9_000),
                paid: 1,
                suspended: None,
                cancelled: false,
            })
        );
        assert_eq!(
            (m.releasable, m.elapsed, m.next_at),
            (Uint128::zero(), 0, Some(t0 + 1))
        );
    }
    assert_eq!(
        f.balance(&dusted),
        tenth + Uint128::one(),
        "an address holding coins is paid like any other"
    );
    assert_eq!(f.member(&stranger).member, None);
    let totals = f.totals();
    assert_eq!(totals.members_issued, 3);
    assert_eq!(totals.members_paid, noah(3_000));
    assert_eq!(totals.escrowed, noah(27_000));
    assert_eq!(totals.balance, noah(27_000));
    assert_eq!(totals.unallocated, Uint128::zero());
    assert_eq!(totals.fees_promised, Uint128::zero());

    // Nothing is due until a period elapses, and a batch that pays nobody
    // fails; a second short of the month is still nothing.
    let err = f.release_members(&stranger, &[&alice, &bob]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);
    f.advance(month);
    assert_eq!(f.member(&alice).releasable, Uint128::zero());
    f.advance(1);
    let m = f.member(&alice);
    assert_eq!(
        (m.releasable, m.elapsed, m.next_at),
        (period, 2, Some(t0 + 1 + 2 * month))
    );
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&alice), tenth + period);
    assert_eq!(f.balance(&bob), tenth + period);
    assert_eq!(
        f.balance(&dusted),
        tenth + Uint128::one(),
        "not in the batch"
    );
    let m = f.member(&alice).member.unwrap();
    assert_eq!(
        (m.paid, m.released, m.remaining),
        (2, tenth + period, noah(8_250))
    );
    assert_eq!(f.totals().members_paid, noah(4_500));
    assert_eq!(f.totals().escrowed, noah(25_500));
    let err = f.release_members(&stranger, &[&alice]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);

    // Three months unclaimed pay in one release, and a member with nothing
    // due is skipped rather than failing the batch.
    f.advance(3 * month);
    f.release_members(&stranger, &[&dusted, &alice]).unwrap();
    assert_eq!(f.balance(&alice), tenth + period * Uint128::new(4));
    assert_eq!(
        f.balance(&dusted),
        tenth + Uint128::one() + period * Uint128::new(4)
    );
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&bob), tenth + period * Uint128::new(4));

    // The year pays out; a paid-out member is skipped.
    f.advance(12 * month);
    f.release_members(&stranger, &[&alice, &bob, &dusted])
        .unwrap();
    for who in [&alice, &bob] {
        assert_eq!(f.balance(who), noah(10_000));
        let m = f.member(who);
        assert_eq!(m.next_at, None);
        let m = m.member.unwrap();
        assert_eq!((m.paid, m.remaining), (13, Uint128::zero()));
    }
    let totals = f.totals();
    assert_eq!(totals.escrowed, Uint128::zero());
    assert_eq!(totals.members_paid, noah(30_000));
    assert_eq!(totals.balance, Uint128::zero());
    let err = f.release_members(&stranger, &[&alice]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);

    // An issued address is never registered again.
    let err = f.register(&[&alice]).unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberExists(alice.to_string())
    );
}

/// The registrar can stop a member's pay and nothing else: a suspended
/// member is skipped, a reinstated one is paid everything since, and a
/// cancel by governance settles what had elapsed by the suspension rather
/// than by the vote, and keeps the rest in the tranche.
#[test]
fn suspension_holds_pay_and_a_cancel_settles_to_it() {
    let mut f = Fixture::new();
    f.fund(noah(20_000));
    let (alice, bob, stranger) = (f.addr("alice"), f.addr("bob"), f.addr("stranger"));
    let registrar = f.registrar.clone();
    let month = 2_628_000;
    let (tenth, period) = (noah(1_000), noah(750));
    f.register(&[&alice, &bob]).unwrap();
    f.advance(1 + 2 * month);
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&alice), tenth + period * Uint128::new(2));

    // Half a month on the registrar suspends alice; only the registrar can,
    // and only once.
    f.advance(month / 2);
    let suspend = ExecuteMsg::SuspendMembers {
        addresses: strings(&[&alice]),
    };
    let err = f.exec(&stranger, suspend.clone()).unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unauthorized(_)
    ));
    f.exec(&registrar, suspend.clone()).unwrap();
    let block = f.app.block_info();
    let m = f.member(&alice);
    assert_eq!(
        m.member.unwrap().suspended,
        Some(Suspension {
            at: block.time.seconds(),
            height: block.height,
            by: registrar.clone(),
        })
    );
    assert_eq!((m.releasable, m.elapsed), (Uint128::zero(), 3));
    let err = f.exec(&registrar, suspend).unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberSuspended(alice.to_string())
    );

    // Two months on bob is paid and alice is skipped; her clock reads at
    // the suspension.
    f.advance(2 * month);
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&alice), tenth + period * Uint128::new(2));
    assert_eq!(f.balance(&bob), tenth + period * Uint128::new(4));
    let err = f.release_members(&stranger, &[&alice]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);
    assert_eq!(f.member(&alice).elapsed, 3);

    // Reinstated, the next release pays everything since.
    let reinstate = |who: &Addr| ExecuteMsg::ReinstateMembers {
        addresses: strings(&[who]),
    };
    let err = f.exec(&stranger, reinstate(&alice)).unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unauthorized(_)
    ));
    let err = f.exec(&registrar, reinstate(&bob)).unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberNotSuspended(bob.to_string())
    );
    f.exec(&registrar, reinstate(&alice)).unwrap();
    let m = f.member(&alice);
    assert_eq!(m.member.unwrap().suspended, None);
    assert_eq!(m.releasable, period * Uint128::new(2));
    f.release_members(&stranger, &[&alice]).unwrap();
    assert_eq!(f.balance(&alice), tenth + period * Uint128::new(4));

    // Suspended again a month later and cancelled a month and a half after
    // that: the month that had elapsed by the suspension settles, the half
    // since and the rest do not, and nothing goes to the pool.
    f.advance(month);
    f.exec(
        &registrar,
        ExecuteMsg::SuspendMembers {
            addresses: strings(&[&alice]),
        },
    )
    .unwrap();
    f.advance(month + month / 2);
    let (contract, pool) = (f.contract.clone(), f.pool.clone());
    f.sudo(SudoMsg::CancelMembers {
        addresses: strings(&[&alice]),
    })
    .unwrap();
    assert_eq!(
        f.balance(&alice),
        tenth + period * Uint128::new(5),
        "period six settled"
    );
    let m = f.member(&alice);
    assert_eq!(m.releasable, Uint128::zero());
    let record = m.member.unwrap();
    assert!(record.cancelled);
    assert_eq!(
        (record.paid, record.released, record.remaining),
        (6, tenth + period * Uint128::new(5), Uint128::zero())
    );
    assert_eq!(f.balance(&pool), Uint128::zero());
    let returned = noah(10_000) - tenth - period * Uint128::new(5);
    let totals = f.totals();
    assert_eq!(
        totals.unallocated, returned,
        "the rest is the tranche's again"
    );
    assert_eq!(
        totals.escrowed,
        noah(10_000) - tenth - period * Uint128::new(4)
    );
    assert_eq!(totals.members_cancelled, 1);
    assert_eq!(
        totals.members_paid,
        tenth * Uint128::new(2) + period * Uint128::new(9)
    );
    assert_eq!(f.balance(&contract), totals.escrowed + returned);

    // A cancelled member is skipped by release and by cancel, and refused
    // by the rest.
    let err = f.release_members(&stranger, &[&alice]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::SuspendMembers {
                addresses: strings(&[&alice]),
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberCancelled(alice.to_string())
    );
    let err = f.exec(&registrar, reinstate(&alice)).unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberCancelled(alice.to_string())
    );
    f.sudo(SudoMsg::CancelMembers {
        addresses: strings(&[&alice]),
    })
    .unwrap();
    assert_eq!(f.balance(&alice), tenth + period * Uint128::new(5));
    assert_eq!(f.totals().members_cancelled, 1, "skipped, not counted");

    // The returned part seats the next member.
    let carol = f.addr("carol");
    let err = f.register(&[&carol]).unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::Unallocated {
            available: returned,
            needed: noah(10_000)
        }
    );
    f.fund(noah(10_000) - returned);
    f.register(&[&carol]).unwrap();
    assert_eq!(f.totals().unallocated, Uint128::zero());

    // A cancel with no suspension settles to the vote.
    f.sudo(SudoMsg::CancelMembers {
        addresses: strings(&[&bob]),
    })
    .unwrap();
    assert_eq!(f.balance(&bob), tenth + period * Uint128::new(7));
    assert_eq!(f.totals().members_cancelled, 2);
}

/// A stolen registrar key can suspend every member and nothing more. The
/// proposal that replaces it voids its suspensions in one write, read
/// wherever a suspension is; the same key set again later suspends afresh.
#[test]
fn a_void_lifts_a_registrars_suspensions_in_one_write() {
    let mut f = Fixture::new();
    f.fund(noah(20_000));
    let (alice, bob, stranger) = (f.addr("alice"), f.addr("bob"), f.addr("stranger"));
    let stolen = f.registrar.clone();
    let month = 2_628_000;
    let (tenth, period) = (noah(1_000), noah(750));
    let suspend = |who: &Addr| ExecuteMsg::SuspendMembers {
        addresses: strings(&[who]),
    };
    f.register(&[&alice, &bob]).unwrap();
    f.advance(1 + month);
    f.exec(
        &stolen,
        ExecuteMsg::SuspendMembers {
            addresses: strings(&[&alice, &bob]),
        },
    )
    .unwrap();
    let err = f.release_members(&stranger, &[&alice, &bob]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);

    // Replaced and voided in the same block as the suspension, since a vote
    // executes after the block's transactions.
    let fresh = f.addr("registrar-2");
    f.sudo(SudoMsg::SetRegistrar {
        registrar: Some(fresh.to_string()),
    })
    .unwrap();
    f.sudo(SudoMsg::VoidSuspensions {
        registrar: stolen.to_string(),
    })
    .unwrap();
    let m = f.member(&alice);
    assert_eq!(m.member.unwrap().suspended, None, "reads as lifted");
    assert_eq!(m.releasable, period);
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&alice), tenth + period);
    assert_eq!(f.balance(&bob), tenth + period);
    // A voided suspension is not one to lift.
    let err = f
        .sudo(SudoMsg::ReinstateMembers {
            addresses: strings(&[&alice]),
        })
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberNotSuspended(alice.to_string())
    );

    // The new registrar suspends over the voided record, and governance
    // lifts that by sudo.
    f.exec(&fresh, suspend(&alice)).unwrap();
    let m = f.member(&alice).member.unwrap();
    assert_eq!(m.suspended.map(|s| s.by), Some(fresh.clone()));
    f.sudo(SudoMsg::ReinstateMembers {
        addresses: strings(&[&alice]),
    })
    .unwrap();
    assert_eq!(f.member(&alice).member.unwrap().suspended, None);

    // The old key, set again in a later block, suspends afresh: the void
    // covered its past.
    f.advance(1);
    f.sudo(SudoMsg::SetRegistrar {
        registrar: Some(stolen.to_string()),
    })
    .unwrap();
    f.exec(&stolen, suspend(&bob)).unwrap();
    assert!(f.member(&bob).member.unwrap().suspended.is_some());
    f.advance(month);
    let err = f.release_members(&stranger, &[&bob]).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingDue);
}

/// A member keeps the schedule they registered under: a changed member grant
/// reaches only the members registered after it.
#[test]
fn a_changed_member_grant_leaves_registered_members_alone() {
    let mut f = Fixture::new();
    f.fund(noah(30_000));
    let (alice, bob, stranger) = (f.addr("alice"), f.addr("bob"), f.addr("stranger"));
    let month = 2_628_000;
    f.register(&[&alice]).unwrap();
    let quarterly: Vec<Period> = (0..4)
        .map(|_| Period {
            length: 3 * month,
            parts: 1,
        })
        .collect();
    f.sudo(SudoMsg::SetMemberGrant {
        amount: noah(20_000),
        schedule: quarterly.clone(),
    })
    .unwrap();
    f.register(&[&bob]).unwrap();
    assert_eq!(
        f.balance(&bob),
        noah(5_000),
        "the first period pays at once whatever its length"
    );
    assert_eq!(f.member(&bob).member.unwrap().schedule, quarterly);
    assert_eq!(f.member(&alice).member.unwrap().schedule, member());

    f.advance(1 + month);
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&alice), noah(1_750));
    assert_eq!(f.balance(&bob), noah(5_000), "no quarter has elapsed");
    f.advance(5 * month);
    f.release_members(&stranger, &[&alice, &bob]).unwrap();
    assert_eq!(f.balance(&alice), noah(1_000 + 6 * 750));
    assert_eq!(f.balance(&bob), noah(10_000));
}

/// Every call over member addresses is bounded like registration and names
/// an address it cannot act on.
#[test]
fn member_batches_are_bounded_and_named() {
    let mut f = Fixture::new();
    f.fund(noah(10_000));
    let (alice, stranger) = (f.addr("alice"), f.addr("stranger"));
    let registrar = f.registrar.clone();
    let many: Vec<String> = (0..=MAX_BATCH)
        .map(|i| f.addr(&format!("m{i}")).to_string())
        .collect();
    f.register(&[&alice]).unwrap();

    let err = f
        .exec(&stranger, ExecuteMsg::ReleaseMembers { addresses: vec![] })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::EmptyBatch);
    let err = f
        .exec(
            &stranger,
            ExecuteMsg::ReleaseMembers {
                addresses: many.clone(),
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::BatchTooLarge(MAX_BATCH + 1, MAX_BATCH)
    );
    let err = f
        .release_members(&stranger, &[&alice, &stranger])
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberNotFound(stranger.to_string())
    );
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::SuspendMembers {
                addresses: strings(&[&stranger]),
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberNotFound(stranger.to_string())
    );
    let err = f
        .sudo(SudoMsg::CancelMembers { addresses: many })
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::BatchTooLarge(MAX_BATCH + 1, MAX_BATCH)
    );

    // A paid-out member cannot be suspended.
    f.advance(1 + 13 * 2_628_000);
    f.release_members(&stranger, &[&alice]).unwrap();
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::SuspendMembers {
                addresses: strings(&[&alice]),
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberPaidOut(alice.to_string())
    );
    // A cancel skips a paid-out member: a last claim while the proposal
    // was open does not fail the batch, and nothing is counted.
    f.sudo(SudoMsg::CancelMembers {
        addresses: strings(&[&alice]),
    })
    .unwrap();
    assert_eq!(f.totals().members_cancelled, 0);
    assert!(!f.member(&alice).member.unwrap().cancelled);
}

/// The window bounds what a registrar key can issue: a batch past the limit
/// is refused whole, one larger than the limit is refused as such, and room
/// returns as registrations age out.
#[test]
fn issuance_is_bounded_per_window() {
    let mut f = Fixture::new();
    f.fund(noah(100_000));
    f.sudo(SudoMsg::SetIssuanceLimit {
        limit: IssuanceLimit {
            max_members: 2,
            window_seconds: 3_600,
        },
    })
    .unwrap();
    let registrar = f.registrar.clone();
    let (a, b, c) = (f.addr("a"), f.addr("b"), f.addr("c"));
    let t0 = f.app.block_info().time.seconds();

    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![a.to_string(), b.to_string(), c.to_string()],
            },
        )
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::BatchTooLarge(3, 2));
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![a.to_string(), b.to_string()],
        },
    )
    .unwrap();
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (2, 0));
    assert_eq!(issuance.entries, vec![IssuanceEntry { at: t0, count: 2 }]);
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![c.to_string()],
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::IssuanceLimit {
            remaining: 0,
            requested: 1,
            fits_at: t0 + 3_600,
        }
    );

    f.app.update_block(|block| {
        block.time = block.time.plus_seconds(3_600);
        block.height += 1;
    });
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (0, 2));
    assert!(issuance.entries.is_empty());
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![c.to_string()],
        },
    )
    .unwrap();

    // A second registration in the block merges into its entry.
    let d = f.addr("d");
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![d.to_string()],
        },
    )
    .unwrap();
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (2, 0));
    assert_eq!(
        issuance.entries,
        vec![IssuanceEntry {
            at: t0 + 3_600,
            count: 2
        }]
    );
}

/// The window slides: a registration counts for window_seconds from its
/// block, so the limit holds over any span of that length and never resets
/// at a boundary.
#[test]
fn issuance_window_slides() {
    let mut f = Fixture::new();
    f.fund(noah(100_000));
    f.sudo(SudoMsg::SetIssuanceLimit {
        limit: IssuanceLimit {
            max_members: 2,
            window_seconds: 3_600,
        },
    })
    .unwrap();
    let registrar = f.registrar.clone();
    let t0 = f.app.block_info().time.seconds();
    let register = |f: &mut Fixture, name: &str| {
        let addr = f.addr(name);
        f.exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![addr.to_string()],
            },
        )
    };
    let advance = |f: &mut Fixture, seconds: u64| {
        f.app.update_block(|block| {
            block.time = block.time.plus_seconds(seconds);
            block.height += 1;
        });
    };

    register(&mut f, "a").unwrap();
    advance(&mut f, 1_800);
    register(&mut f, "b").unwrap();
    advance(&mut f, 1_800);

    // An hour after the first registration it has aged out and the second
    // has not: one seat is free, where a window reset here would free two.
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!(issuance.window_start, t0);
    assert_eq!((issuance.issued, issuance.remaining), (1, 1));
    assert_eq!(
        issuance.entries,
        vec![IssuanceEntry {
            at: t0 + 1_800,
            count: 1
        }]
    );
    register(&mut f, "c").unwrap();
    let err = register(&mut f, "d").unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::IssuanceLimit {
            remaining: 0,
            requested: 1,
            fits_at: t0 + 5_400,
        }
    );

    advance(&mut f, 1_800);
    register(&mut f, "d").unwrap();
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (2, 0));
    assert_eq!(
        issuance.entries,
        vec![
            IssuanceEntry {
                at: t0 + 3_600,
                count: 1
            },
            IssuanceEntry {
                at: t0 + 5_400,
                count: 1
            },
        ]
    );
}

/// A grant too small to give every vesting period a coin is refused where it
/// is set, since the chain rejects a period without one.
#[test]
fn grants_too_small_for_their_schedule_are_refused() {
    let mut f = Fixture::new();
    // Enough for the grant's gas, so the schedule is what refuses it.
    f.fund(noah(3));
    let err = f
        .sudo(SudoMsg::SetMemberGrant {
            amount: Uint128::new(10),
            schedule: standard(),
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));
    let grantee = f.addr("tiny");
    let err = f
        .sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: Uint128::new(10),
            schedule: standard(),
            seat_holder: false,
            release_address: None,
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));
    assert_eq!(f.totals().escrowed, Uint128::zero());

    // A tranche the rules do not release yet is checked all the same: the
    // remainder over the unit here would be a tranche of ten anoah.
    f.fund(noah(1_000_005));
    f.set_bonded(noah(150_000_000));
    let err = f
        .sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: noah(1_000_000) + Uint128::new(10),
            schedule: standard(),
            seat_holder: true,
            release_address: None,
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));
    assert_eq!(f.totals().escrowed, Uint128::zero());
}

#[test]
fn registration_refusals() {
    let mut f = Fixture::new();
    f.fund(noah(10_000));
    let registrar = f.registrar.clone();
    let stranger = f.addr("stranger");
    let carol = f.addr("carol");

    let err = f
        .exec(
            &stranger,
            ExecuteMsg::RegisterMembers {
                addresses: vec![carol.to_string()],
            },
        )
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unauthorized(_)
    ));

    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers { addresses: vec![] },
        )
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::EmptyBatch);

    let many: Vec<String> = (0..=MAX_BATCH)
        .map(|i| f.addr(&format!("m{i}")).to_string())
        .collect();
    let err = f
        .exec(&registrar, ExecuteMsg::RegisterMembers { addresses: many })
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::BatchTooLarge(MAX_BATCH + 1, MAX_BATCH)
    );

    // Two grants need 20,000; the tranche holds 10,000.
    let dave = f.addr("dave");
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![carol.to_string(), dave.to_string()],
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::Unallocated {
            available: noah(10_000),
            needed: noah(20_000)
        }
    );

    // The same address twice in one batch is refused before anything is sent.
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![carol.to_string(), carol.to_string()],
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberExists(carol.to_string())
    );
    assert_eq!(f.account(&carol), None);

    // Governance can disable registration.
    f.sudo(SudoMsg::SetRegistrar { registrar: None }).unwrap();
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![carol.to_string()],
            },
        )
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unauthorized(_)
    ));
}

/// The plan's worked example: a 30M grant while others hold 50M pays 10M at
/// once, 10M more once they hold 100M, and the last 10M at 150M.
#[test]
fn contributor_grant_escrows_and_releases_by_the_cap() {
    let mut f = Fixture::new();
    // The grant and two NOAH of gas for each of its thirty possible tranches.
    f.fund(noah(30_000_060));
    let grantee = f.addr("contributor");

    f.sudo(SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(30_000_000),
        schedule: standard(),
        seat_holder: false,
        release_address: None,
    })
    .unwrap();

    let (_, periods, total) = f.account(&grantee).expect("first tranche created");
    assert_eq!(periods, 37);
    assert_eq!(total, noah(10_000_000));
    let g = f.grant(1);
    assert_eq!(g.released, noah(10_000_000));
    assert_eq!(g.remaining, noah(20_000_000));
    let (reserve, next) = f.ownership(1);
    assert_eq!(next, None, "the first address is spent");
    assert_eq!(
        f.allowance(&grantee),
        Some(noah(2)),
        "the tranche can pay its gas"
    );
    assert_eq!(reserve, noah(58), "one allowance promised from the reserve");
    let totals = f.totals();
    assert_eq!(totals.escrowed, noah(20_000_000));
    assert_eq!(totals.fees_reserved, noah(58));
    assert_eq!(totals.fees_promised, noah(2));
    assert_eq!(totals.unallocated, Uint128::zero());
    assert_eq!(totals.contributors_paid, noah(10_000_000));

    // Nothing more until others' stake grows, and no address to pay to anyway.
    let err = f.exec(&grantee, ExecuteMsg::Release { id: 1 }).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NoReleaseAddress(1));

    let second = f.addr("contributor-2");
    let stranger = f.addr("stranger");
    let err = f
        .exec(
            &stranger,
            ExecuteMsg::SetReleaseAddress {
                id: 1,
                address: second.to_string(),
            },
        )
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unauthorized(_)
    ));
    f.exec(
        &grantee,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: second.to_string(),
        },
    )
    .unwrap();

    assert_eq!(f.releasable(1).amount, Uint128::zero());
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 1 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingReleasable(1));

    // Others hold 100M: bonded is that plus the grantee's own 10M.
    f.set_bonded(noah(110_000_000));
    let (own, cap, _) = f.cap(1);
    assert_eq!(own, noah(10_000_000));
    assert_eq!(cap, noah(20_000_000));
    assert_eq!(f.releasable(1).amount, noah(10_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&second).unwrap().2, noah(10_000_000));
    assert_eq!(f.allowance(&second), Some(noah(2)));
    assert_eq!(f.totals().fees_promised, noah(4));
    assert_eq!(f.totals().fees_reserved, noah(56));
    assert_eq!(f.grant(1).remaining, noah(10_000_000));

    // A release into an address that already exists is rejected and the
    // address cleared; the grant is untouched.
    let third = f.addr("contributor-3");
    f.mark_existing(&third);
    f.exec(
        &grantee,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: third.to_string(),
        },
    )
    .unwrap();
    f.set_bonded(noah(170_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.grant(1).remaining, noah(10_000_000));
    assert_eq!(f.ownership(1).1, None);
    assert_eq!(
        f.allowance(&third),
        None,
        "a rejected tranche grants nothing"
    );

    let fourth = f.addr("contributor-4");
    f.exec(
        &grantee,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: fourth.to_string(),
        },
    )
    .unwrap();
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&fourth).unwrap().2, noah(10_000_000));
    let g = f.grant(1);
    assert_eq!(g.remaining, Uint128::zero());
    assert_eq!(g.released, noah(30_000_000));
    assert_eq!(f.totals().escrowed, Uint128::zero());
    // Three tranches used six; the last returned the rest of the reserve.
    assert_eq!(f.ownership(1).0, Uint128::zero());
    assert_eq!(f.totals().fees_reserved, Uint128::zero());
    assert_eq!(f.balance(&f.pool.clone()), noah(54), "unused gas returned");
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 1 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::Exhausted(1));

    // A grant arrives with its own spend and gas, so promised gas never
    // blocks it: it checks free, not unallocated.
    assert_eq!(f.totals().unallocated, Uint128::zero());
    assert_eq!(f.totals().fees_promised, noah(6));
    let late = f.addr("late");
    f.fund(noah(1_000_002));
    f.sudo(SudoMsg::AddGrant {
        grantee: late.to_string(),
        amount: noah(1_000_000),
        schedule: standard(),
        seat_holder: false,
        release_address: None,
    })
    .unwrap();
    assert_eq!(f.account(&late).unwrap().2, noah(1_000_000));
    assert_eq!(f.totals().fees_promised, noah(8));
}

/// The founder's table: nothing releases while bonded stake is under 150M;
/// then the bloc rule pays 5M at 165M, 5M more at 180M, and 15M more at
/// 225M, the seat and every release counting as own.
#[test]
fn seat_holder_waits_on_the_bloc_rule() {
    let mut f = Fixture::new();
    f.fund(noah(60_000_120));
    let founder = f.addr("founder");
    f.set_bonded(noah(150_000_000));

    f.sudo(SudoMsg::AddGrant {
        grantee: founder.to_string(),
        amount: noah(60_000_000),
        schedule: standard(),
        seat_holder: true,
        release_address: None,
    })
    .unwrap();
    assert_eq!(f.account(&founder), None, "nothing releases at 150M");
    let (own, _, seat_allowance) = f.cap(1);
    assert_eq!(own, noah(5_000_000));
    assert_eq!(seat_allowance, Some(Uint128::zero()));
    assert_eq!(f.totals().escrowed, noah(60_000_000));

    let stranger = f.addr("stranger");
    f.set_bonded(noah(165_000_000));
    assert_eq!(f.releasable(1).amount, noah(5_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&founder).unwrap().2, noah(5_000_000));
    assert_eq!(f.cap(1).0, noah(10_000_000));

    let second = f.addr("founder-2");
    f.exec(
        &founder,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: second.to_string(),
        },
    )
    .unwrap();
    f.set_bonded(noah(180_000_000));
    assert_eq!(f.releasable(1).amount, noah(5_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&second).unwrap().2, noah(5_000_000));

    let third = f.addr("founder-3");
    f.exec(
        &founder,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: third.to_string(),
        },
    )
    .unwrap();
    f.set_bonded(noah(225_000_000));
    assert_eq!(f.releasable(1).amount, noah(15_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.grant(1).released, noah(25_000_000));

    // A second seat holder shares the room pro rata by what each has left.
    let other = f.addr("seat-two");
    f.fund(noah(35_000_070));
    f.set_bonded(noah(240_000_000));
    f.sudo(SudoMsg::AddGrant {
        grantee: other.to_string(),
        amount: noah(35_000_000),
        schedule: standard(),
        seat_holder: true,
        release_address: None,
    })
    .unwrap();
    // Room at add time: 80M − 50M − 25M received = 5M, shared 35:35 with the
    // founder's remaining, so 2.5M, paid as 2M in whole millions.
    assert_eq!(
        f.account(&other).unwrap().2,
        noah(2_000_000),
        "released in whole millions"
    );
    assert_eq!(f.grant(2).remaining, noah(33_000_000));
    // Afterwards the room is 3M, shared 35:33.
    assert_eq!(
        f.cap(2).2,
        Some(noah(3_000_000).multiply_ratio(33u64, 68u64))
    );
    assert_eq!(
        f.releasable(2).amount,
        noah(1_000_000),
        "floored to a whole million"
    );
}

#[test]
fn ownership_ceiling_excludes_the_seat_and_spans_grants() {
    for seat_holder in [false, true] {
        let mut f = Fixture::new();
        f.set_bonded(noah(400_000_000));
        f.fund(noah(60_000_120));
        let grantee = f.addr("contributor");
        f.sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: noah(60_000_000),
            schedule: standard(),
            seat_holder,
            release_address: None,
        })
        .unwrap();

        let own = noah(if seat_holder { 65_000_000 } else { 60_000_000 });
        assert_eq!(f.grant(1).released, noah(60_000_000));
        assert_eq!(f.grant(1).remaining, Uint128::zero());
        assert_eq!(f.account(&grantee).unwrap().2, noah(60_000_000));
        assert_eq!((f.cap(1).0, f.cap(1).1), (own, own));

        // A fresh grant shares the person's ceiling even with more stake.
        f.set_bonded(noah(1_000_000_000));
        f.fund(noah(1_000_002));
        let next = f.addr("contributor-next");
        f.sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: noah(1_000_000),
            schedule: standard(),
            seat_holder,
            release_address: Some(next.to_string()),
        })
        .unwrap();
        assert_eq!(f.grant(2).released, Uint128::zero());
        assert_eq!(f.grant(2).remaining, noah(1_000_000));
        assert_eq!(f.releasable(2).amount, Uint128::zero());
        assert_eq!((f.cap(2).0, f.cap(2).1), (own, own));
        assert_eq!(f.account(&next), None);
        let err = f.exec(&grantee, ExecuteMsg::Release { id: 2 }).unwrap_err();
        assert_eq!(contract_error(&err), &ContractError::NothingReleasable(2));
    }
}

#[test]
fn cancel_returns_the_unreleased_part_and_return_unallocated_is_bounded() {
    let mut f = Fixture::new();
    f.fund(noah(40_000_000));
    let grantee = f.addr("contributor");
    f.sudo(SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(30_000_000),
        schedule: standard(),
        seat_holder: false,
        release_address: None,
    })
    .unwrap();
    // The 10M over escrow, less the reserve's other 58 and the 2 promised.
    assert_eq!(f.totals().unallocated, noah(9_999_940));

    let pool = f.pool.clone();
    let err = f
        .sudo(SudoMsg::ReturnUnallocated {
            amount: noah(10_000_001),
        })
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unallocated { .. }
    ));
    f.sudo(SudoMsg::ReturnUnallocated {
        amount: noah(4_000_000),
    })
    .unwrap();
    assert_eq!(f.balance(&pool), noah(4_000_000));

    f.sudo(SudoMsg::CancelGrant { id: 1 }).unwrap();
    assert_eq!(
        f.balance(&pool),
        noah(24_000_058),
        "the 20M escrow and its unused gas came back"
    );
    let g = f.grant(1);
    assert!(g.cancelled);
    assert_eq!(g.remaining, Uint128::zero());
    assert_eq!(
        g.released,
        noah(10_000_000),
        "released coins are the grantee's"
    );
    assert_eq!(f.totals().escrowed, Uint128::zero());
    assert_eq!(f.totals().fees_reserved, Uint128::zero());
    assert_eq!(f.totals().unallocated, noah(5_999_940));

    let err = f.exec(&grantee, ExecuteMsg::Release { id: 1 }).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::Cancelled(1));
    let err = f.sudo(SudoMsg::CancelGrant { id: 1 }).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::Cancelled(1));
    let err = f.sudo(SudoMsg::CancelGrant { id: 9 }).unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::GrantNotFound(9));
}

#[test]
fn add_grant_needs_unallocated_balance_and_a_valid_schedule() {
    let mut f = Fixture::new();
    f.fund(noah(1_000_000));
    let grantee = f.addr("contributor");
    let err = f
        .sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: noah(2_000_000),
            schedule: standard(),
            seat_holder: false,
            release_address: None,
        })
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::Unallocated {
            available: noah(1_000_000),
            needed: noah(2_000_004)
        }
    );
    let err = f
        .sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: noah(1),
            schedule: vec![],
            seat_holder: false,
            release_address: None,
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));

    // A scoped grant under the cap pays whole, no escrow, on its own schedule.
    let monthly: Vec<Period> = (0..24)
        .map(|_| Period {
            length: 2_628_000,
            parts: 1,
        })
        .collect();
    f.sudo(SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(200_000),
        schedule: monthly,
        seat_holder: false,
        release_address: None,
    })
    .unwrap();
    let (_, periods, total) = f.account(&grantee).unwrap();
    assert_eq!(periods, 24);
    assert_eq!(total, noah(200_000));
    assert_eq!(f.totals().escrowed, Uint128::zero());
}

/// A grantee the contract has paid holds an account, which cannot take a
/// vesting account: a repeat grant waits for a release address rather than
/// burning its first tranche on the grantee, and a proposal may name one.
#[test]
fn a_repeat_grant_waits_for_an_address_and_a_proposal_may_name_one() {
    let mut f = Fixture::new();
    f.fund(noah(3_000_012));
    let (grantee, stranger) = (f.addr("contributor"), f.addr("stranger"));
    let grant = |grantee: &Addr, release_address: Option<&Addr>| SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(1_000_000),
        schedule: standard(),
        seat_holder: false,
        release_address: release_address.map(|a| a.to_string()),
    };
    f.sudo(grant(&grantee, None)).unwrap();
    assert_eq!(f.account(&grantee).unwrap().2, noah(1_000_000));

    // Known to the contract: nothing is attempted and the grant waits.
    f.sudo(grant(&grantee, None)).unwrap();
    assert_eq!(f.ownership(2), (noah(2), None));
    assert_eq!(f.grant(2).remaining, noah(1_000_000));
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 2 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NoReleaseAddress(2));
    let second = f.addr("contributor-2");
    f.exec(
        &grantee,
        ExecuteMsg::SetReleaseAddress {
            id: 2,
            address: second.to_string(),
        },
    )
    .unwrap();
    f.exec(&stranger, ExecuteMsg::Release { id: 2 }).unwrap();
    assert_eq!(f.account(&second).unwrap().2, noah(1_000_000));
    assert_eq!(f.ownership(2), (Uint128::zero(), None));

    // Named in the proposal: the first tranche goes there at once, for a
    // grantee whose own address already holds an account.
    let founder = f.addr("founder");
    f.mark_existing(&founder);
    let fresh = f.addr("founder-2");
    f.sudo(grant(&founder, Some(&fresh))).unwrap();
    assert_eq!(f.account(&fresh).unwrap().2, noah(1_000_000));
    assert_eq!(f.grant(3).remaining, Uint128::zero());
    assert_eq!(f.ownership(3), (Uint128::zero(), None));
}

/// A raised allowance does not reach a grant reserved at the old rate and
/// does not starve its last tranches either: each tranche draws the reserve
/// spread over the tranches the grant can still take.
#[test]
fn a_raised_allowance_spreads_an_old_reserve_over_its_tranches() {
    let mut f = Fixture::new();
    f.fund(noah(3_000_006));
    let (grantee, stranger) = (f.addr("contributor"), f.addr("stranger"));
    // A fifth of 5M is a million: one unit a tranche.
    f.set_bonded(noah(5_000_000));
    f.sudo(SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(3_000_000),
        schedule: standard(),
        seat_holder: false,
        release_address: None,
    })
    .unwrap();
    assert_eq!(f.account(&grantee).unwrap().2, noah(1_000_000));
    assert_eq!(f.allowance(&grantee), Some(noah(2)));
    assert_eq!(f.ownership(1).0, noah(4));

    f.sudo(SudoMsg::SetFeeAllowance { amount: noah(5) })
        .unwrap();
    let second = f.addr("contributor-2");
    f.exec(
        &grantee,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: second.to_string(),
        },
    )
    .unwrap();
    f.set_bonded(noah(11_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&second).unwrap().2, noah(1_000_000));
    assert_eq!(
        f.allowance(&second),
        Some(noah(2)),
        "two a tranche, not five"
    );
    assert_eq!(f.ownership(1).0, noah(2));

    let third = f.addr("contributor-3");
    f.exec(
        &grantee,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: third.to_string(),
        },
    )
    .unwrap();
    f.set_bonded(noah(17_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&third).unwrap().2, noah(1_000_000));
    assert_eq!(
        f.allowance(&third),
        Some(noah(2)),
        "the last tranche keeps its gas"
    );
    assert_eq!(f.ownership(1).0, Uint128::zero());
    assert_eq!(f.totals().fees_reserved, Uint128::zero());
    assert_eq!(f.totals().fees_promised, noah(6));
}

#[test]
fn fee_allowance_is_set_by_sudo_and_reserved_at_grant() {
    let mut f = Fixture::new();
    f.sudo(SudoMsg::SetFeeAllowance { amount: noah(5) })
        .unwrap();
    let c: ConfigResponse = f.query(QueryMsg::Config {});
    assert_eq!(c.fee_allowance, noah(5));

    // A grant under the unit is one tranche: its amount and one allowance.
    let grantee = f.addr("late");
    f.fund(noah(200_004));
    let grant = SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(200_000),
        schedule: standard(),
        seat_holder: false,
        release_address: None,
    };
    let err = f.sudo(grant.clone()).unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::Unallocated {
            available: noah(200_004),
            needed: noah(200_005)
        }
    );
    f.fund(noah(1));
    f.sudo(grant).unwrap();
    assert_eq!(f.allowance(&grantee), Some(noah(5)));
    assert_eq!(f.totals().fees_promised, noah(5));
}

/// A stream stays in the contract and pays by the clock: nothing at once,
/// each period to the payee by bank send as it elapses, whole periods only,
/// and a cancel holds what has elapsed for the payee and returns the rest.
#[test]
fn stream_pays_by_the_clock_and_cancel_returns_what_has_not_elapsed() {
    let mut f = Fixture::new();
    f.fund(noah(262_000));
    let hire = f.addr("hire");
    let stranger = f.addr("stranger");
    let month = 2_628_000;
    let monthly: Vec<Period> = (0..24)
        .map(|_| Period {
            length: month,
            parts: 1,
        })
        .collect();
    let period = noah(262_000).multiply_ratio(1u64, 24u64);
    let t0 = f.app.block_info().time.seconds();
    let advance = |f: &mut Fixture, seconds: u64| {
        f.app.update_block(|block| {
            block.time = block.time.plus_seconds(seconds);
            block.height += 1;
        });
    };

    f.sudo(SudoMsg::AddStream {
        grantee: hire.to_string(),
        amount: noah(262_000),
        schedule: monthly,
    })
    .unwrap();
    assert_eq!(f.account(&hire), None, "no vesting account");
    assert_eq!(f.balance(&hire), Uint128::zero(), "nothing pays at once");
    let g = f.grant(1);
    assert_eq!(
        g.kind,
        Kind::Stream {
            start: t0,
            paid: 0,
            payee: hire.clone()
        }
    );
    assert_eq!(g.remaining, noah(262_000));
    let totals = f.totals();
    assert_eq!(totals.escrowed, noah(262_000));
    assert_eq!(totals.unallocated, Uint128::zero());
    assert_eq!(
        totals.fees_reserved,
        Uint128::zero(),
        "a stream reserves no gas"
    );
    let r = f.releasable(1);
    assert_eq!(r.amount, Uint128::zero());
    assert_eq!(
        r.rule,
        Rule::Clock {
            start: t0,
            elapsed: 0,
            paid: 0,
            next_at: Some(t0 + month)
        }
    );
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 1 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingReleasable(1));

    // A second short of the month is still nothing; the month itself pays.
    advance(&mut f, month - 1);
    assert_eq!(f.releasable(1).amount, Uint128::zero());
    advance(&mut f, 1);
    assert_eq!(f.releasable(1).amount, period);
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.balance(&hire), period);
    assert_eq!(
        f.account(&hire),
        None,
        "paid by send, not a vesting account"
    );
    let g = f.grant(1);
    assert_eq!(g.released, period);
    assert_eq!(
        g.kind,
        Kind::Stream {
            start: t0,
            paid: 1,
            payee: hire.clone()
        }
    );
    assert_eq!(f.totals().contributors_paid, period);
    assert_eq!(f.totals().escrowed, noah(262_000) - period);
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 1 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::NothingReleasable(1));

    // Three months unclaimed pay in one release.
    advance(&mut f, 3 * month);
    let r = f.releasable(1);
    assert_eq!(r.amount, period * Uint128::new(3));
    assert_eq!(
        r.rule,
        Rule::Clock {
            start: t0,
            elapsed: 4,
            paid: 1,
            next_at: Some(t0 + 5 * month)
        }
    );
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.balance(&hire), period * Uint128::new(4));

    // The controller moves the pay to another address, which is kept.
    let wallet = f.addr("hire-wallet");
    let err = f
        .exec(
            &stranger,
            ExecuteMsg::SetReleaseAddress {
                id: 1,
                address: wallet.to_string(),
            },
        )
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unauthorized(_)
    ));
    f.exec(
        &hire,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: wallet.to_string(),
        },
    )
    .unwrap();
    advance(&mut f, month);
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.balance(&wallet), period);
    assert_eq!(
        f.grant(1).kind,
        Kind::Stream {
            start: t0,
            paid: 5,
            payee: wallet.clone()
        }
    );
    advance(&mut f, month);
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(
        f.balance(&wallet),
        period * Uint128::new(2),
        "the address is kept"
    );

    // Cancelled a month and a half on: the month is held for the payee, so
    // no payee can fail the cancel, and the half returns.
    advance(&mut f, month + month / 2);
    let pool = f.pool.clone();
    let contract = f.contract.clone();
    f.sudo(SudoMsg::CancelGrant { id: 1 }).unwrap();
    assert_eq!(
        f.balance(&wallet),
        period * Uint128::new(2),
        "held, not sent"
    );
    let paid = period * Uint128::new(7);
    assert_eq!(f.balance(&pool), noah(262_000) - paid, "the rest came back");
    assert_eq!(f.balance(&contract), period, "the elapsed month is held");
    let g = f.grant(1);
    assert!(g.cancelled);
    assert_eq!(g.released, period * Uint128::new(6));
    assert_eq!(g.remaining, period);
    assert_eq!(
        g.kind,
        Kind::Stream {
            start: t0,
            paid: 7,
            payee: wallet.clone()
        }
    );
    assert_eq!(f.totals().escrowed, period);
    let r = f.releasable(1);
    assert_eq!(r.amount, period);
    assert_eq!(
        r.rule,
        Rule::Clock {
            start: t0,
            elapsed: 7,
            paid: 7,
            next_at: None
        }
    );

    // The controller may still point the held pay, and anyone releases it;
    // then the stream is spent.
    let other = f.addr("hire-other");
    f.exec(
        &hire,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: other.to_string(),
        },
    )
    .unwrap();
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.balance(&other), period);
    assert_eq!(f.balance(&contract), Uint128::zero());
    let g = f.grant(1);
    assert_eq!((g.released, g.remaining), (paid, Uint128::zero()));
    assert_eq!(f.totals().escrowed, Uint128::zero());
    assert_eq!(f.totals().contributors_paid, paid);
    assert_eq!(f.releasable(1).amount, Uint128::zero());
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 1 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::Cancelled(1));
    let err = f
        .exec(
            &hire,
            ExecuteMsg::SetReleaseAddress {
                id: 1,
                address: wallet.to_string(),
            },
        )
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::Exhausted(1));
}

/// Streams are pay, not ownership: they check neither the cap nor the bloc
/// rule, count in neither own nor the seat pool, and share a person with an
/// ownership grant to the same address.
#[test]
fn streams_stay_outside_the_ownership_rules() {
    let mut f = Fixture::new();
    f.fund(noah(60_262_120));
    let founder = f.addr("founder");
    let stranger = f.addr("stranger");
    let month = 2_628_000;
    let monthly: Vec<Period> = (0..24)
        .map(|_| Period {
            length: month,
            parts: 1,
        })
        .collect();
    let period = noah(262_000).multiply_ratio(1u64, 24u64);
    f.set_bonded(noah(150_000_000));

    f.sudo(SudoMsg::AddGrant {
        grantee: founder.to_string(),
        amount: noah(60_000_000),
        schedule: standard(),
        seat_holder: true,
        release_address: None,
    })
    .unwrap();
    let before = f.cap(1);
    assert_eq!(
        before,
        (noah(5_000_000), noah(29_000_000), Some(Uint128::zero()))
    );
    f.sudo(SudoMsg::AddStream {
        grantee: founder.to_string(),
        amount: noah(262_000),
        schedule: monthly.clone(),
    })
    .unwrap();
    assert_eq!(f.cap(1), before, "the stream moved nothing");
    assert_eq!(f.totals().escrowed, noah(60_262_000));

    // The stream pays while the bloc rule still holds the ownership grant.
    f.app.update_block(|block| {
        block.time = block.time.plus_seconds(month);
        block.height += 1;
    });
    f.exec(&stranger, ExecuteMsg::Release { id: 2 }).unwrap();
    assert_eq!(f.balance(&founder), period);
    let p: PersonResponse = f.query(QueryMsg::Person {
        grantee: founder.to_string(),
    });
    assert!(p.seat_holder);
    assert_eq!(p.released, Uint128::zero(), "pay is not own");
    assert_eq!(p.own, noah(5_000_000));

    // At 165M the seat pool is 5M, shared by ownership escrow alone: with
    // the stream's remaining in the pool the share would floor to 4M.
    f.set_bonded(noah(165_000_000));
    assert_eq!(f.releasable(1).amount, noah(5_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&founder).unwrap().2, noah(5_000_000));
    let p: PersonResponse = f.query(QueryMsg::Person {
        grantee: founder.to_string(),
    });
    assert_eq!(p.released, noah(5_000_000));

    // A stream first makes the address a person without a seat; the next
    // ownership grant restates it.
    let hire = f.addr("hire");
    f.fund(noah(262_000));
    f.sudo(SudoMsg::AddStream {
        grantee: hire.to_string(),
        amount: noah(262_000),
        schedule: monthly.clone(),
    })
    .unwrap();
    let p: PersonResponse = f.query(QueryMsg::Person {
        grantee: hire.to_string(),
    });
    assert_eq!((p.controller, p.seat_holder), (hire.clone(), false));

    // Refusals: zero, an empty schedule, too small for its periods, over free.
    let err = f
        .sudo(SudoMsg::AddStream {
            grantee: hire.to_string(),
            amount: Uint128::zero(),
            schedule: monthly.clone(),
        })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::ZeroAmount);
    let err = f
        .sudo(SudoMsg::AddStream {
            grantee: hire.to_string(),
            amount: noah(1),
            schedule: vec![],
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));
    let err = f
        .sudo(SudoMsg::AddStream {
            grantee: hire.to_string(),
            amount: Uint128::new(10),
            schedule: monthly.clone(),
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));
    let err = f
        .sudo(SudoMsg::AddStream {
            grantee: hire.to_string(),
            amount: noah(1_000_000),
            schedule: monthly,
        })
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::Unallocated { .. }
    ));
}
