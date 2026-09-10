# Ark Client Fee Construction

- Status: **Current as of the dynamic-fee decorator; D78/D80 settled**
- Last updated: 2026-09-05
- Target SDK: cosmos-sdk v0.54.3
- Audience: wallet, exchange, custody, and relayer authors building Ark transactions outside `arkd`

This note is what you need to know before reading `proto/ark/treasury/v1/query.proto`. Ark charges a transfer tax
and prices gas from consensus state, so a fee built the way it is built on a stock Cosmos chain will be refused.
The chain publishes every number you need; what it does not publish — and cannot — is how much to pad. That
judgement is yours, and this document tells you what it is defending against.

Everything here describes `app/ante/fee.go`. Where the two disagree, the code is right and this document is a bug.

## 1. The four rules

**The fee is a ceiling, not a payment (D80).** You declare a maximum; the chain charges the exact transfer tax
and exactly one base fee under it, and the remainder never leaves the account. Over-declaring a stable leg costs
nothing. This is the single most important difference from Terra Classic, where the declared fee was deducted
whole — habits ported from there will make you under-declare.

**A fee short of the tax is refused outright (D78).** The check runs before anything moves and is per
denomination: for every coin in the computed tax, the fee must carry at least that amount in that denomination.
Miss one leg and the whole transaction is refused, however generous the rest of the fee is. The tax is a floor;
everything above it is slack.

**The numbers move between building and inclusion, so pad.** Treasury runs a base-fee controller that adjusts the
price by at most `BaseFeeAdjustmentRate` per block — 2.5% by default, governable up to a doubling. The tax can
move too: a governance change to the rate, or a cap re-derived from fresh oracle rates, lands between your query
and your inclusion. A fee that is exactly right when you build it can be short when it arrives. Because of rule
one this costs you nothing to defend against, and one pad defends both — lifting a stable leg raises the amount
the tax check sees as well as the slack the base fee is taken from.

**NOAH is charged whole; the remainder is the tip.** `anoah` is the one denomination that does not behave as a
ceiling. Whatever you declare in NOAH leaves the account: the base fee if no stable leg covered it, and the entire
rest as a tip. Padding NOAH is a donation. Padding anything else is free. NOAH is also never taxed, which is what
lets a tip and a padded tax be told apart at all.

## 2. What the chain publishes

Four calls, all with gateway routes, so a TypeScript client needs no gRPC-web and no Go.

| what | RPC | route |
| --- | --- | --- |
| tax the messages owe, and the principal it was computed over | `ark.treasury.v1.Query/ComputeTax` | `POST /ark/treasury/v1/compute_tax` |
| the whole gas price sheet | `ark.treasury.v1.Query/GasPrices` | `GET /ark/treasury/v1/gas_prices` |
| one denomination's gas price | `ark.treasury.v1.Query/GasPrice` | `GET /ark/treasury/v1/gas_prices/{denom}` |
| what the payer can spend | `cosmos.bank.v1beta1.Query/SpendableBalances` | SDK standard |

Both Treasury queries are `module_query_safe`, so contracts can call them too.

`ComputeTax` takes the messages as packed `Any`s and returns the tax as `Coins`, beside `tax_base`: the principal
it was computed over, summed per denomination — which denominations the transaction already moves, and how much.
The tax is authoritative: it calls the same `Keeper.ComputeTax` the ante calls, on the same state. Do not reimplement it — Terra Classic's client-side copy of
this arithmetic drifted from its own ante, and the gap became a standing tax dodge.

`GasPrices` returns every accepted non-reference denomination in `gas_prices` (NOAH among them; order is
presentation only) and the reference row in its own `reference_gas_price` field. Each `GasPrice` carries:

- `denom`
- `gas_price` — a `LegacyDec` of **denom base units per gas unit**, with the denomination's conversion factor
  already applied. Multiply by your gas limit and ceil; no further conversion.
- `derived_height` — the block whose rates produced the conversion factor, so staleness is observable. Zero for
  the reference denomination, whose cross is the identity.

A denomination whose price is not representable is omitted from the sheet. The sheet lists what the gate accepts,
so a denomination absent from it will not pay a base fee.

The gas limit itself is the SDK's ordinary `Simulate`. Ark's fee decorator meters under simulation and charges a
38,000 gas allowance for the fee transfer a fee-less estimate cannot make. It covers a stablecoin base fee plus
a NOAH tip; fee-less estimates for single-denomination gas fees are more conservative. A simulation carrying a
payable fee meters the actual transfer instead.

## 3. Building a fee

1. **Simulate** for the gas limit, then apply a gas adjustment. `arkd` uses 1.15.
2. **`ComputeTax`** on the exact messages you are about to sign. Call it on the final messages — a change to any
   amount changes the tax.
3. **`GasPrices`**, and pick a denomination the payer can spend. `required = ceil(gas_price × gas_limit)`. A
   denomination in `tax_base` is one the payer is already spending, and the natural first choice.
4. **Declare** `tax + required`, with the stable legs padded and NOAH exact.

Illustrative, sending `ausd` and paying in `ausd` (base units, `1e18 = 1 USD`):

| part | value |
| --- | --- |
| simulated gas × 1.15 | 138 000 |
| `gas_price` for `ausd` | 1e11 per gas unit |
| `required` | 1.38e16 `ausd` |
| `ComputeTax` | 5e18 `ausd` |
| declared, padded 1.1× | 5.5152e18 `ausd` |
| actually charged | 5.0138e18 `ausd` |

The difference stays in the account. Real declarations are integers in base units.

## 4. What the ante does with your declaration

Understanding this is what lets you pad confidently rather than superstitiously.

**The tax check first.** Per denomination, refused whole if short. Nothing has moved yet.

**Then one leg pays the base fee.** The ante walks the fee's legs **in the order you encoded them**, skipping
NOAH. For each, `slack = amount − tax(denom)`; if the slack is positive and covers that denomination's
`required`, that leg pays, and the walk stops. A leg with no conversion factor, or one whose slack falls short, is
skipped rather than refused — it is simply a tax-only ceiling, charged its tax and nothing more.

> **The encoded order names the paying leg.** `sdk.NewCoins` sorts, and most client libraries sort for you, which
> yields denomination order by default. If you care which leg pays, construct the coin list directly. Unsorted fee
> coins are valid: the tx's `ValidateBasic` rejects nil and negative amounts, not unsortedness.

**Then NOAH.** If no stable leg covered the base fee and the NOAH leg is at least the NOAH requirement, NOAH pays
it. Either way the rest of the NOAH leg is the tip.

**If nothing covered it, the transaction is refused**, and the error quotes the requirement in reference units.
A NOAH leg the price table cannot price is also refused — a tip nothing can rank buys nothing.

**Priority is the tip alone**, expressed in reference units per gas unit. A stable leg's slack and the tax do not
buy block space. If you want inclusion priority, tip in NOAH; padding `ausd` will not help.

## 5. What is taxed

The tax base is the conversion-factor set: a denomination is taxed exactly when Treasury holds a factor for it.
`anoah` is never taxed. A denomination outside the registry, or a member whose first refresh has not landed, is
untaxed. The cap (`TaxCap` / `TaxCaps`) applies **independently to each message input**, not once per transaction.

These messages present a taxable input:

- `cosmos.bank.v1beta1.MsgSend`, `MsgMultiSend` (per input)
- `cosmos.vesting.v1beta1.MsgCreateVestingAccount`, `MsgCreatePermanentLockedAccount`,
  `MsgCreatePeriodicVestingAccount` (the whole schedule as one input) — funding a vesting account is a transfer,
  and leaving it untaxed was Terra Classic's standard dodge
- `ark.market.v1.MsgSwapSend` (the offer coin). Plain `MsgSwap` is **not** taxed
- `ibc.applications.transfer.v1.MsgTransfer` and IBC v2 `MsgSendPacket` — the outbound leg only; forwarded hops,
  acknowledgements, timeouts, and refunds are never re-presented (D46)
- `cosmwasm.wasm.v1.MsgExecuteContract`, `MsgInstantiateContract`, `MsgInstantiateContract2`,
  `MsgStoreAndInstantiateContract` (attached funds)
- `cosmos.authz.v1beta1.MsgExec`, recursively

Treat this list as orientation and `ComputeTax` as the answer. The list moves; the query does not lie.

## 6. Cases that behave differently

- **Fee grants.** A granter bears base fee, tip, and tax together, each drawn on the allowance as it is charged.
  It is strict: a grant that will not cover a charge fails the transaction rather than falling back to the payer.
  Size allowances against the padded declaration, not the expected charge.
- **Simulation.** Nothing is refused on fee grounds; the tax is set aside and the reads execution makes are made,
  so the estimate carries their cost.
- **Zero gas.** Refused outside simulation.
- **Genesis (height 0).** No tax and no gate — gentxs carry no fees. Irrelevant to live clients, listed so that
  genesis tooling is not read as the live rule.

## 7. Client policy choices

The chain publishes tax and gas-price requirements; each client chooses headroom, gas adjustment, and fee denomination.
Those choices are not consensus rules. [arkd's reference implementation](../../cmd/arkd/README.md#cli-fee-completion)
documents its defaults and selection behaviour alongside the command code.

## 8. Why there is no `EstimateFee` RPC

Because everything above is already available and authoritative, and the only thing such an RPC would add is the
prudent half — which would then be fixed on Ark's release cadence rather than yours, visible to contracts, and
uniform across integrations whose risk appetites are not. Consolidating the round trips is not worth that: gas
still has to be simulated separately, so a one-call estimate would save two requests, not four.

If your integration would genuinely be better served by a single priced-fee call, that is worth hearing — but it
is a request for a shared default, and the default is what this document is for.
