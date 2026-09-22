//! Flow tests on cw-multi-test with a stub for the chain: the stub creates
//! vesting accounts by moving the coins, refuses an address that already
//! holds an account, records fee allowances, and answers the pool query with
//! whatever bonded stake the test sets.

use anyhow::bail;
use ark_grant::contract::{execute, instantiate, migrate, query, reply, sudo, MAX_BATCH};
use ark_grant::msg::{
    CapRule, ConfigResponse, ExecuteMsg, GrantResponse, InstantiateMsg, IssuanceLimit,
    IssuanceResponse, MemberResponse, MemberStatus, Period, QueryMsg, ReleasableResponse, SudoMsg,
    TotalsResponse,
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
                    member_schedule: standard(),
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
}

fn contract_error(err: &anyhow::Error) -> &ContractError {
    err.downcast_ref::<ContractError>()
        .unwrap_or_else(|| panic!("not a contract error: {err:?}"))
}

#[test]
fn instantiate_records_config() {
    let f = Fixture::new();
    let config: ConfigResponse = f.query(QueryMsg::Config {});
    assert_eq!(config.registrar, Some(f.registrar.clone()));
    assert_eq!(config.member_grant, noah(10_000));
    assert_eq!(config.member_schedule.len(), 37);
    let totals = f.totals();
    assert_eq!(totals.balance, Uint128::zero());
    assert_eq!(totals.escrowed, Uint128::zero());
}

#[test]
fn members_issue_at_registration_and_a_dusted_address_is_skipped() {
    let mut f = Fixture::new();
    f.fund(noah(30_000));
    let (alice, bob, dusted) = (f.addr("alice"), f.addr("bob"), f.addr("dusted"));
    f.mark_existing(&dusted);

    let registrar = f.registrar.clone();
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![alice.to_string(), dusted.to_string(), bob.to_string()],
        },
    )
    .unwrap();

    for member in [&alice, &bob] {
        let status: MemberResponse = f.query(QueryMsg::Member {
            address: member.to_string(),
        });
        let (start, periods, total) = f
            .account(member)
            .unwrap_or_else(|| panic!("account not created: {status:?}"));
        assert_eq!(start, f.app.block_info().time.seconds() as i64);
        assert_eq!(periods, 37);
        assert_eq!(total, noah(10_000));
        assert_eq!(f.balance(member), noah(10_000));
        assert_eq!(f.allowance(member), Some(noah(2)), "fee allowance granted");
        let m: MemberResponse = f.query(QueryMsg::Member {
            address: member.to_string(),
        });
        assert!(matches!(m.status, Some(MemberStatus::Issued { .. })));
    }
    let m: MemberResponse = f.query(QueryMsg::Member {
        address: dusted.to_string(),
    });
    match m.status {
        Some(MemberStatus::Rejected { error, .. }) => {
            assert!(error.contains("already exists"), "{error}")
        }
        other => panic!("dusted address is {other:?}"),
    }
    assert_eq!(f.allowance(&dusted), None);

    let totals = f.totals();
    assert_eq!(totals.members_issued, 2);
    assert_eq!(totals.members_rejected, 1);
    assert_eq!(
        totals.balance,
        noah(10_000),
        "the rejected grant stayed unallocated"
    );
    assert_eq!(totals.unallocated, noah(10_000));

    // Neither an issued nor a rejected address can be registered again.
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![alice.to_string()],
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberExists(alice.to_string())
    );
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![dusted.to_string()],
            },
        )
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::MemberExists(dusted.to_string())
    );
}

/// The window bounds what a registrar key can issue: a batch past the limit
/// is refused whole, rejected registrations count, and a fresh window opens
/// once the old one elapses.
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

    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![a.to_string(), b.to_string(), c.to_string()],
            },
        )
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::IssuanceLimit {
            remaining: 2,
            requested: 3,
            ..
        }
    ));
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![a.to_string(), b.to_string()],
        },
    )
    .unwrap();
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (2, 0));
    let err = f
        .exec(
            &registrar,
            ExecuteMsg::RegisterMembers {
                addresses: vec![c.to_string()],
            },
        )
        .unwrap_err();
    assert!(matches!(
        contract_error(&err),
        ContractError::IssuanceLimit {
            remaining: 0,
            requested: 1,
            ..
        }
    ));

    f.app.update_block(|block| {
        block.time = block.time.plus_seconds(3_600);
        block.height += 1;
    });
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (0, 2));
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![c.to_string()],
        },
    )
    .unwrap();

    // A rejected registration counts against the window too.
    let dusted = f.addr("dusted-2");
    f.mark_existing(&dusted);
    f.exec(
        &registrar,
        ExecuteMsg::RegisterMembers {
            addresses: vec![dusted.to_string()],
        },
    )
    .unwrap();
    let issuance: IssuanceResponse = f.query(QueryMsg::Issuance {});
    assert_eq!((issuance.issued, issuance.remaining), (2, 0));
}

/// A grant too small to give every vesting period a coin is refused where it
/// is set, since the chain rejects a period without one.
#[test]
fn grants_too_small_for_their_schedule_are_refused() {
    let mut f = Fixture::new();
    f.fund(noah(1));
    let err = f
        .sudo(SudoMsg::SetMemberGrant {
            amount: Uint128::new(10),
            schedule: standard(),
            fee_allowance: Uint128::zero(),
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
            paid_elsewhere: Uint128::zero(),
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
    f.fund(noah(30_000_000));
    let grantee = f.addr("contributor");

    f.sudo(SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(30_000_000),
        schedule: standard(),
        seat_holder: false,
        paid_elsewhere: Uint128::zero(),
    })
    .unwrap();

    let (_, periods, total) = f.account(&grantee).expect("first tranche created");
    assert_eq!(periods, 37);
    assert_eq!(total, noah(10_000_000));
    let g = f.grant(1);
    assert_eq!(g.released, noah(10_000_000));
    assert_eq!(g.remaining, noah(20_000_000));
    assert_eq!(g.release_address, None, "the first address is spent");
    let totals = f.totals();
    assert_eq!(totals.escrowed, noah(20_000_000));
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
    let r = f.releasable(1);
    assert_eq!(r.own, noah(10_000_000));
    assert_eq!(r.cap, noah(20_000_000));
    assert_eq!(r.amount, noah(10_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&second).unwrap().2, noah(10_000_000));
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
    assert_eq!(f.grant(1).release_address, None);

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
    let err = f
        .exec(&stranger, ExecuteMsg::Release { id: 1 })
        .unwrap_err();
    assert_eq!(contract_error(&err), &ContractError::Exhausted(1));
}

/// The founder's table: the seat and the first 5M count as own, the bloc
/// rule holds everything until bonded stake passes 150M, and then releases
/// 5M at 180M and 15M more at 225M.
#[test]
fn seat_holder_waits_on_the_bloc_rule() {
    let mut f = Fixture::new();
    f.fund(noah(55_000_000));
    let founder = f.addr("founder");
    f.set_bonded(noah(165_000_000));

    f.sudo(SudoMsg::AddGrant {
        grantee: founder.to_string(),
        amount: noah(55_000_000),
        schedule: standard(),
        seat_holder: true,
        paid_elsewhere: noah(5_000_000),
    })
    .unwrap();
    assert_eq!(f.account(&founder), None, "nothing releases at 165M");
    let r = f.releasable(1);
    assert_eq!(r.own, noah(10_000_000));
    assert_eq!(r.seat_allowance, Some(Uint128::zero()));
    assert_eq!(f.totals().escrowed, noah(55_000_000));

    let stranger = f.addr("stranger");
    f.set_bonded(noah(180_000_000));
    assert_eq!(f.releasable(1).amount, noah(5_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.account(&founder).unwrap().2, noah(5_000_000));

    let next = f.addr("founder-2");
    f.exec(
        &founder,
        ExecuteMsg::SetReleaseAddress {
            id: 1,
            address: next.to_string(),
        },
    )
    .unwrap();
    f.set_bonded(noah(225_000_000));
    assert_eq!(f.releasable(1).amount, noah(15_000_000));
    f.exec(&stranger, ExecuteMsg::Release { id: 1 }).unwrap();
    assert_eq!(f.grant(1).released, noah(20_000_000));

    // A second seat holder shares the room pro rata by what each has left.
    let other = f.addr("seat-two");
    f.fund(noah(35_000_000));
    f.set_bonded(noah(240_000_000));
    f.sudo(SudoMsg::AddGrant {
        grantee: other.to_string(),
        amount: noah(35_000_000),
        schedule: standard(),
        seat_holder: true,
        paid_elsewhere: Uint128::zero(),
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
    let r = f.releasable(2);
    assert_eq!(
        r.seat_allowance,
        Some(noah(3_000_000).multiply_ratio(33u64, 68u64))
    );
    assert_eq!(r.amount, noah(1_000_000), "floored to a whole million");
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
        paid_elsewhere: Uint128::zero(),
    })
    .unwrap();
    assert_eq!(f.totals().unallocated, noah(10_000_000));

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
        noah(24_000_000),
        "the 20M escrow came back"
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
    assert_eq!(f.totals().unallocated, noah(6_000_000));

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
            paid_elsewhere: Uint128::zero(),
        })
        .unwrap_err();
    assert_eq!(
        contract_error(&err),
        &ContractError::Unallocated {
            available: noah(1_000_000),
            needed: noah(2_000_000)
        }
    );
    let err = f
        .sudo(SudoMsg::AddGrant {
            grantee: grantee.to_string(),
            amount: noah(1),
            schedule: vec![],
            seat_holder: false,
            paid_elsewhere: Uint128::zero(),
        })
        .unwrap_err();
    assert!(matches!(contract_error(&err), ContractError::Schedule(_)));

    // A scoped grant under the cap pays whole, no escrow, on its own schedule.
    let stream: Vec<Period> = (0..24)
        .map(|_| Period {
            length: 2_628_000,
            parts: 1,
        })
        .collect();
    f.sudo(SudoMsg::AddGrant {
        grantee: grantee.to_string(),
        amount: noah(200_000),
        schedule: stream,
        seat_holder: false,
        paid_elsewhere: Uint128::zero(),
    })
    .unwrap();
    let (_, periods, total) = f.account(&grantee).unwrap();
    assert_eq!(periods, 24);
    assert_eq!(total, noah(200_000));
    assert_eq!(f.totals().escrowed, Uint128::zero());
}
