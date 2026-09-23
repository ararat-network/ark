//! The plan's arithmetic, kept pure so it can be tested without a chain.
//! Every figure floors, and every ratio multiplies before it divides.

use cosmwasm_std::{StdError, Uint128};

use crate::error::ContractError;
use crate::msg::{CapRule, IssuanceEntry, IssuanceLimit, Period};

/// MAX_SCHEDULE_PERIODS bounds a schedule at a century of monthly periods.
pub const MAX_SCHEDULE_PERIODS: usize = 1_200;
/// MAX_PERIOD_LENGTH bounds one period at a century, so a schedule's end
/// time stays far inside the SDK's int64.
pub const MAX_PERIOD_LENGTH: u64 = 100 * 365 * 86_400;

pub fn validate_schedule(schedule: &[Period]) -> Result<(), ContractError> {
    if schedule.is_empty() {
        return Err(ContractError::Schedule("at least one period".into()));
    }
    if schedule.len() > MAX_SCHEDULE_PERIODS {
        return Err(ContractError::Schedule(format!(
            "{} periods, more than {}",
            schedule.len(),
            MAX_SCHEDULE_PERIODS
        )));
    }
    let mut total: u64 = 0;
    for (i, p) in schedule.iter().enumerate() {
        if p.length == 0 {
            return Err(ContractError::Schedule(format!("period {i}: zero length")));
        }
        if p.length > MAX_PERIOD_LENGTH {
            return Err(ContractError::Schedule(format!(
                "period {i}: length exceeds {MAX_PERIOD_LENGTH}"
            )));
        }
        if p.parts == 0 {
            return Err(ContractError::Schedule(format!("period {i}: zero parts")));
        }
        total = total
            .checked_add(p.parts)
            .ok_or_else(|| ContractError::Schedule("parts overflow".into()))?;
    }
    Ok(())
}

pub fn validate_issuance_limit(limit: &IssuanceLimit) -> Result<(), ContractError> {
    if limit.max_members == 0 {
        return Err(ContractError::Config(
            "issuance limit must admit members".into(),
        ));
    }
    if limit.window_seconds == 0 {
        return Err(ContractError::Config(
            "issuance window must be positive".into(),
        ));
    }
    Ok(())
}

/// in_window drops the entries that have aged out: one at `at` counts until
/// `at + window_seconds`.
pub fn in_window(
    mut entries: Vec<IssuanceEntry>,
    now: u64,
    window_seconds: u64,
) -> Vec<IssuanceEntry> {
    entries.retain(|e| e.at.saturating_add(window_seconds) > now);
    entries
}

/// issued is what the window's entries hold.
pub fn issued(entries: &[IssuanceEntry]) -> u64 {
    entries.iter().fold(0, |sum, e| sum.saturating_add(e.count))
}

/// fits_at is when a batch of `requested` fits under the limit: now if it
/// does, else when enough of the oldest entries have aged out, and None if
/// the batch alone exceeds the limit.
pub fn fits_at(
    entries: &[IssuanceEntry],
    now: u64,
    limit: &IssuanceLimit,
    requested: u64,
) -> Option<u64> {
    if requested > limit.max_members {
        return None;
    }
    let needed = issued(entries)
        .saturating_add(requested)
        .saturating_sub(limit.max_members);
    if needed == 0 {
        return Some(now);
    }
    let mut freed: u64 = 0;
    for e in entries {
        freed = freed.saturating_add(e.count);
        if freed >= needed {
            return Some(e.at.saturating_add(limit.window_seconds));
        }
    }
    // Unreachable: needed is at most what the entries hold.
    None
}

pub fn validate_cap(cap: &CapRule) -> Result<(), ContractError> {
    if cap.numerator == 0 || cap.denominator == 0 {
        return Err(ContractError::Config("cap share must be positive".into()));
    }
    if cap.numerator >= cap.denominator {
        return Err(ContractError::Config("cap share must be below one".into()));
    }
    if cap.ceiling.is_zero() {
        return Err(ContractError::Config("cap ceiling must be positive".into()));
    }
    if cap.unit.is_zero() {
        return Err(ContractError::Config("cap unit must be positive".into()));
    }
    Ok(())
}

/// split apportions amount across a validated schedule by parts. Every period
/// but the last floors amount × parts / total, and the last takes the
/// remainder, so the periods sum to amount exactly. An amount too small to
/// give every period a coin is refused: the chain rejects a period without.
pub fn split(amount: Uint128, schedule: &[Period]) -> Result<Vec<Uint128>, ContractError> {
    let total: u64 = schedule.iter().map(|p| p.parts).sum();
    let mut out = Vec::with_capacity(schedule.len());
    let mut remaining = amount;
    let last = schedule.len() - 1;
    for (i, p) in schedule.iter().enumerate() {
        let share = if i == last {
            remaining
        } else {
            amount.multiply_ratio(p.parts, total)
        };
        if share.is_zero() {
            return Err(ContractError::Schedule(format!(
                "period {i} would carry no coins: {amount} is too small for the schedule"
            )));
        }
        out.push(share);
        remaining = remaining.checked_sub(share)?;
    }
    Ok(out)
}

/// cap_total bounds seat and released ownership grants together by a share
/// of the bonded stake they do not hold. The ceiling bounds grants alone,
/// so the total ceiling includes the seat. own is assumed bonded and
/// subtracted whole, which can only lower the cap.
pub fn cap_total(
    bonded: Uint128,
    own: Uint128,
    seat: Uint128,
    cap: &CapRule,
) -> Result<Uint128, ContractError> {
    let others = bonded.saturating_sub(own);
    let ceiling = cap.ceiling.checked_add(seat)?;
    Ok(others
        .multiply_ratio(cap.numerator, cap.denominator)
        .min(ceiling))
}

/// seat_allowance is what seat holders together may still receive: a third
/// of bonded stake less the founding stake and less what they already have,
/// the bloc rule against the number the contract can read.
pub fn seat_allowance(bonded: Uint128, founding_stake: Uint128, seat_grants: Uint128) -> Uint128 {
    bonded
        .multiply_ratio(1u64, 3u64)
        .saturating_sub(founding_stake)
        .saturating_sub(seat_grants)
}

/// pro_rata is allowance × part / total, floored; zero when total is zero.
pub fn pro_rata(allowance: Uint128, part: Uint128, total: Uint128) -> Uint128 {
    if total.is_zero() {
        return Uint128::zero();
    }
    allowance.multiply_ratio(part, total)
}

/// release_amount pays a grant whole when it fits under its allowance, and
/// otherwise the allowance floored to whole units.
pub fn release_amount(remaining: Uint128, allowance: Uint128, unit: Uint128) -> Uint128 {
    if remaining <= allowance {
        return remaining;
    }
    allowance
        .multiply_ratio(1u64, unit)
        .checked_mul(unit)
        .unwrap_or(Uint128::zero())
}

/// tranches is the most a grant of amount can still take: every release is
/// a whole unit or the remainder, so ceil(amount / unit).
fn tranches(amount: Uint128, unit: Uint128) -> Result<Uint128, ContractError> {
    Ok(amount
        .checked_add(unit)?
        .checked_sub(Uint128::one())?
        .checked_div(unit)
        .map_err(StdError::from)?)
}

/// smallest_tranche is the least one release of an ownership grant can be:
/// the remainder over whole units, or a unit when there is none. A split
/// that works at an amount works at every larger one, so a grant whose
/// smallest tranche splits releases every tranche.
pub fn smallest_tranche(amount: Uint128, unit: Uint128) -> Result<Uint128, ContractError> {
    let rest = amount.checked_rem(unit).map_err(StdError::from)?;
    Ok(if rest.is_zero() { unit } else { rest })
}

/// fee_reserve is one allowance per tranche a grant can take at most.
pub fn fee_reserve(
    amount: Uint128,
    unit: Uint128,
    allowance: Uint128,
) -> Result<Uint128, ContractError> {
    Ok(allowance.checked_mul(tranches(amount, unit)?)?)
}

/// tranche_gas is what one tranche draws from its grant's reserve: the
/// allowance, or the reserve spread over this tranche and those the
/// remaining amount can still take, so a raised allowance does not starve
/// the last tranches of a grant reserved at the old rate.
pub fn tranche_gas(
    reserve: Uint128,
    allowance: Uint128,
    remaining: Uint128,
    unit: Uint128,
) -> Result<Uint128, ContractError> {
    let left = tranches(remaining, unit)?.checked_add(Uint128::one())?;
    Ok(allowance.min(reserve.checked_div(left).map_err(StdError::from)?))
}

/// elapsed is how many leading periods of schedule have ended by now,
/// counted from start, and when the next ends, none once all have.
pub fn elapsed(schedule: &[Period], start: u64, now: u64) -> (u32, Option<u64>) {
    let mut end = start;
    for (i, p) in schedule.iter().enumerate() {
        end = end.saturating_add(p.length);
        if now < end {
            return (i as u32, Some(end));
        }
    }
    (schedule.len() as u32, None)
}

/// accrued is a stream's pay for periods paid..elapsed: slices of split, so
/// each period is exact and all of them sum to the grant.
pub fn accrued(
    amount: Uint128,
    schedule: &[Period],
    paid: u32,
    elapsed: u32,
) -> Result<Uint128, ContractError> {
    let mut sum = Uint128::zero();
    for share in split(amount, schedule)?
        .into_iter()
        .take(elapsed as usize)
        .skip(paid as usize)
    {
        sum = sum.checked_add(share)?;
    }
    Ok(sum)
}

#[cfg(test)]
mod tests {
    use super::*;

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

    fn cap() -> CapRule {
        CapRule {
            numerator: 1,
            denominator: 5,
            ceiling: noah(60_000_000),
            unit: noah(1_000_000),
        }
    }

    #[test]
    fn schedule_validation() {
        assert!(validate_schedule(&standard()).is_ok());
        assert!(validate_schedule(&[]).is_err());
        assert!(validate_schedule(&[Period {
            length: 0,
            parts: 1
        }])
        .is_err());
        assert!(validate_schedule(&[Period {
            length: 1,
            parts: 0
        }])
        .is_err());
        let long: Vec<Period> = (0..=MAX_SCHEDULE_PERIODS)
            .map(|_| Period {
                length: 1,
                parts: 1,
            })
            .collect();
        assert!(validate_schedule(&long).is_err());
        assert!(validate_schedule(&long[..MAX_SCHEDULE_PERIODS]).is_ok());
        assert!(validate_schedule(&[Period {
            length: MAX_PERIOD_LENGTH + 1,
            parts: 1
        }])
        .is_err());
        assert!(validate_schedule(&[Period {
            length: MAX_PERIOD_LENGTH,
            parts: 1
        }])
        .is_ok());
    }

    #[test]
    fn issuance_window_slides() {
        let entry = |at, count| IssuanceEntry { at, count };
        let limit = IssuanceLimit {
            max_members: 3,
            window_seconds: 100,
        };
        let log = vec![entry(10, 2), entry(50, 1)];
        assert_eq!(in_window(log.clone(), 109, 100), log);
        assert_eq!(in_window(log.clone(), 110, 100), vec![entry(50, 1)]);
        assert!(in_window(log.clone(), 150, 100).is_empty());
        assert_eq!(issued(&log), 3);

        // Full at 60: one more fits when the first entry ages out, three
        // when both have, and four never at this limit.
        assert_eq!(fits_at(&log, 60, &limit, 1), Some(110));
        assert_eq!(fits_at(&log, 60, &limit, 2), Some(110));
        assert_eq!(fits_at(&log, 60, &limit, 3), Some(150));
        assert_eq!(fits_at(&log, 60, &limit, 4), None);
        assert_eq!(fits_at(&[entry(10, 1)], 60, &limit, 2), Some(60));
        assert_eq!(fits_at(&[], 60, &limit, 3), Some(60));

        // A limit lowered under what the window holds frees nothing until
        // enough has aged out.
        let lowered = IssuanceLimit {
            max_members: 2,
            window_seconds: 100,
        };
        assert_eq!(fits_at(&log, 60, &lowered, 1), Some(110));
        assert_eq!(fits_at(&log, 60, &lowered, 2), Some(150));
    }

    #[test]
    fn split_standard_member_grant() {
        let periods = split(noah(10_000), &standard()).unwrap();
        assert_eq!(periods.len(), 37);
        assert_eq!(periods[0], noah(2_500));
        let month = noah(10_000).multiply_ratio(1u64, 48u64);
        for p in &periods[1..36] {
            assert_eq!(*p, month);
        }
        assert!(periods[36] > month, "the last month takes the remainder");
        let sum: Uint128 = periods.iter().sum();
        assert_eq!(sum, noah(10_000));
    }

    #[test]
    fn split_remainder_and_small_amounts() {
        let three = vec![
            Period {
                length: 1,
                parts: 1
            };
            3
        ];
        assert_eq!(
            split(Uint128::new(10), &three).unwrap(),
            vec![3u128, 3, 4]
                .into_iter()
                .map(Uint128::new)
                .collect::<Vec<_>>()
        );
        // Too small for three periods: the chain refuses a period without coins.
        assert!(split(Uint128::new(1), &three).is_err());
        assert!(split(Uint128::new(2), &three).is_err());
        assert_eq!(
            split(Uint128::new(3), &three).unwrap(),
            vec![1u128, 1, 1]
                .into_iter()
                .map(Uint128::new)
                .collect::<Vec<_>>()
        );
        let weighted = vec![
            Period {
                length: 1,
                parts: 12,
            },
            Period {
                length: 1,
                parts: 1,
            },
            Period {
                length: 1,
                parts: 3,
            },
        ];
        assert_eq!(
            split(Uint128::new(100), &weighted).unwrap(),
            vec![75u128, 6, 19]
                .into_iter()
                .map(Uint128::new)
                .collect::<Vec<_>>()
        );
    }

    #[test]
    fn cap_at_genesis_and_ceiling() {
        for (name, bonded, own, seat, expected) in [
            ("genesis", 50_000_000, 0, 0, 10_000_000),
            (
                "seat counts towards share",
                294_000_000,
                30_000_000,
                5_000_000,
                52_800_000,
            ),
            ("non-seat ceiling", 400_000_000, 10_000_000, 0, 60_000_000),
            (
                "seat ceiling",
                400_000_000,
                65_000_000,
                5_000_000,
                65_000_000,
            ),
            (
                "seat share boundary",
                390_000_000,
                65_000_000,
                5_000_000,
                65_000_000,
            ),
            (
                "seat share below boundary",
                385_000_000,
                65_000_000,
                5_000_000,
                64_000_000,
            ),
            ("own exceeds bonded", 1, 2, 0, 0),
        ] {
            assert_eq!(
                cap_total(noah(bonded), noah(own), noah(seat), &cap()).unwrap(),
                noah(expected),
                "{name}"
            );
        }
        let overflowing = CapRule {
            ceiling: Uint128::MAX,
            ..cap()
        };
        assert!(matches!(
            cap_total(Uint128::MAX, Uint128::one(), Uint128::one(), &overflowing),
            Err(ContractError::Overflow(_))
        ));
    }

    /// The review's colluder case: two people granting in turns under the
    /// earlier "quarter of the total" rule crossed a third on their second
    /// grant. Under a fifth of what others hold, each converges on a quarter
    /// of the base and the pair on a third, never past it.
    #[test]
    fn two_colluders_stay_at_a_third() {
        let base = noah(50_000_000);
        let mut a = Uint128::zero();
        let mut b = Uint128::zero();
        for _ in 0..50 {
            let bonded = base + a + b;
            a = cap_total(bonded, a, Uint128::zero(), &cap()).unwrap();
            let bonded = base + a + b;
            b = cap_total(bonded, b, Uint128::zero(), &cap()).unwrap();
        }
        let bonded = base + a + b;
        let pair = (a + b).multiply_ratio(1_000_000u64, bonded);
        assert!(
            pair <= Uint128::new(333_334),
            "pair holds {pair} millionths"
        );
        assert!(a <= base.multiply_ratio(1u64, 4u64) && b <= base.multiply_ratio(1u64, 4u64));
    }

    #[test]
    fn seat_allowance_follows_the_bloc_rule() {
        // The plan's founder table: 180M bonded lets seat holders reach 10M.
        assert_eq!(
            seat_allowance(noah(180_000_000), noah(50_000_000), Uint128::zero()),
            noah(10_000_000)
        );
        assert_eq!(
            seat_allowance(noah(180_000_000), noah(50_000_000), noah(5_000_000)),
            noah(5_000_000)
        );
        // Below 150M there is no room at all.
        assert_eq!(
            seat_allowance(noah(150_000_000), noah(50_000_000), Uint128::zero()),
            Uint128::zero()
        );
        assert_eq!(
            seat_allowance(noah(50_000_000), noah(50_000_000), Uint128::zero()),
            Uint128::zero()
        );
    }

    #[test]
    fn release_rounds_to_units() {
        let unit = noah(1_000_000);
        // A 30M grant against a 10M allowance releases 10M.
        assert_eq!(
            release_amount(noah(30_000_000), noah(10_000_000), unit),
            noah(10_000_000)
        );
        // An allowance of 10.9M releases 10M.
        assert_eq!(
            release_amount(noah(30_000_000), noah(10_900_000), unit),
            noah(10_000_000)
        );
        // A grant that fits pays whole, below the unit or not.
        assert_eq!(
            release_amount(noah(200_000), noah(10_000_000), unit),
            noah(200_000)
        );
        assert_eq!(
            release_amount(noah(2_500_000), noah(2_500_000), unit),
            noah(2_500_000)
        );
        // Under a unit of allowance releases nothing.
        assert_eq!(
            release_amount(noah(30_000_000), noah(999_999), unit),
            Uint128::zero()
        );
    }

    #[test]
    fn fee_reserve_is_one_allowance_a_tranche() {
        let unit = noah(1_000_000);
        // 30M in whole millions is thirty tranches at most.
        assert_eq!(
            fee_reserve(noah(30_000_000), unit, noah(2)).unwrap(),
            noah(60)
        );
        // A remainder is a tranche of its own.
        assert_eq!(
            fee_reserve(noah(2_500_000), unit, noah(2)).unwrap(),
            noah(6)
        );
        // Under a unit pays whole, one tranche.
        assert_eq!(fee_reserve(noah(200_000), unit, noah(2)).unwrap(), noah(2));
        assert_eq!(
            fee_reserve(noah(30_000_000), unit, Uint128::zero()).unwrap(),
            Uint128::zero()
        );
        assert!(fee_reserve(noah(1), Uint128::zero(), noah(2)).is_err());
    }

    #[test]
    fn smallest_tranche_is_the_remainder_or_a_unit() {
        let unit = noah(1_000_000);
        assert_eq!(smallest_tranche(noah(30_000_000), unit).unwrap(), unit);
        assert_eq!(
            smallest_tranche(noah(2_500_000), unit).unwrap(),
            noah(500_000)
        );
        assert_eq!(
            smallest_tranche(noah(200_000), unit).unwrap(),
            noah(200_000)
        );
        // A remainder of ten anoah is a tranche the schedule cannot carry.
        let odd = noah(1_000_000) + Uint128::new(10);
        assert_eq!(smallest_tranche(odd, unit).unwrap(), Uint128::new(10));
        assert!(split(Uint128::new(10), &standard()).is_err());
        assert!(smallest_tranche(odd, Uint128::zero()).is_err());
    }

    #[test]
    fn tranche_gas_spreads_the_reserve() {
        let unit = noah(1_000_000);
        // Thirty tranches reserved at two: a 10M first tranche leaves twenty
        // more, so the reserve spreads over twenty-one. The allowance binds
        // at two; raised to five, the spread does, and no tranche starves.
        assert_eq!(
            tranche_gas(noah(60), noah(2), noah(20_000_000), unit).unwrap(),
            noah(2)
        );
        assert_eq!(
            tranche_gas(noah(60), noah(5), noah(20_000_000), unit).unwrap(),
            noah(60).multiply_ratio(1u64, 21u64)
        );
        // A lowered allowance draws less and leaves the rest for the end.
        assert_eq!(
            tranche_gas(noah(60), noah(1), noah(20_000_000), unit).unwrap(),
            noah(1)
        );
        // The last tranche may take the whole reserve, up to the allowance.
        assert_eq!(
            tranche_gas(noah(2), noah(5), Uint128::zero(), unit).unwrap(),
            noah(2)
        );
        assert_eq!(
            tranche_gas(noah(7), noah(5), Uint128::zero(), unit).unwrap(),
            noah(5)
        );
        assert_eq!(
            tranche_gas(Uint128::zero(), noah(2), noah(1), unit).unwrap(),
            Uint128::zero()
        );
    }

    #[test]
    fn pro_rata_shares_floor() {
        assert_eq!(
            pro_rata(Uint128::new(10), Uint128::new(1), Uint128::new(3)),
            Uint128::new(3)
        );
        assert_eq!(
            pro_rata(Uint128::new(10), Uint128::new(3), Uint128::new(3)),
            Uint128::new(10)
        );
        assert_eq!(
            pro_rata(Uint128::new(10), Uint128::new(1), Uint128::zero()),
            Uint128::zero()
        );
    }

    #[test]
    fn elapsed_counts_whole_periods() {
        let monthly: Vec<Period> = (0..3)
            .map(|_| Period {
                length: 100,
                parts: 1,
            })
            .collect();
        assert_eq!(elapsed(&monthly, 1_000, 1_000), (0, Some(1_100)));
        assert_eq!(elapsed(&monthly, 1_000, 1_099), (0, Some(1_100)));
        assert_eq!(elapsed(&monthly, 1_000, 1_100), (1, Some(1_200)));
        assert_eq!(elapsed(&monthly, 1_000, 1_250), (2, Some(1_300)));
        assert_eq!(elapsed(&monthly, 1_000, 1_300), (3, None));
        assert_eq!(elapsed(&monthly, 1_000, 9_999), (3, None));
        // Uneven periods count by their own lengths.
        let uneven = vec![
            Period {
                length: 10,
                parts: 1,
            },
            Period {
                length: 100,
                parts: 1,
            },
        ];
        assert_eq!(elapsed(&uneven, 0, 9), (0, Some(10)));
        assert_eq!(elapsed(&uneven, 0, 10), (1, Some(110)));
        assert_eq!(elapsed(&uneven, 0, 109), (1, Some(110)));
        assert_eq!(elapsed(&uneven, 0, 110), (2, None));
    }

    #[test]
    fn accrued_is_exact_slices_of_the_split() {
        let three = vec![
            Period {
                length: 1,
                parts: 1
            };
            3
        ];
        // split(10) is 3, 3, 4.
        let ten = Uint128::new(10);
        assert_eq!(accrued(ten, &three, 0, 0).unwrap(), Uint128::zero());
        assert_eq!(accrued(ten, &three, 0, 1).unwrap(), Uint128::new(3));
        assert_eq!(accrued(ten, &three, 1, 3).unwrap(), Uint128::new(7));
        assert_eq!(accrued(ten, &three, 0, 3).unwrap(), ten);
        assert_eq!(accrued(ten, &three, 3, 3).unwrap(), Uint128::zero());
        // The plan's stream: twenty-four months, the last taking the remainder.
        let monthly: Vec<Period> = (0..24)
            .map(|_| Period {
                length: 2_628_000,
                parts: 1,
            })
            .collect();
        let month = noah(262_000).multiply_ratio(1u64, 24u64);
        assert_eq!(accrued(noah(262_000), &monthly, 0, 1).unwrap(), month);
        assert_eq!(
            accrued(noah(262_000), &monthly, 0, 24).unwrap(),
            noah(262_000)
        );
        assert!(accrued(noah(262_000), &monthly, 23, 24).unwrap() > month);
        assert!(
            accrued(Uint128::new(10), &monthly, 0, 1).is_err(),
            "too small for the schedule"
        );
    }
}
