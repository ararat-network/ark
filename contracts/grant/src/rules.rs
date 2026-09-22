//! The plan's arithmetic, kept pure so it can be tested without a chain.
//! Every figure floors, and every ratio multiplies before it divides.

use cosmwasm_std::Uint128;

use crate::error::ContractError;
use crate::msg::{CapRule, IssuanceLimit, Period};

/// MAX_SCHEDULE_PERIODS bounds a schedule at a century of monthly periods.
pub const MAX_SCHEDULE_PERIODS: usize = 1_200;

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
        if p.length > i64::MAX as u64 {
            return Err(ContractError::Schedule(format!(
                "period {i}: length overflows"
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

/// cap_total is the most a person may have received in total: a share of the
/// bonded stake they do not hold, at most the ceiling. own is assumed bonded
/// and subtracted whole, which can only lower the cap.
pub fn cap_total(bonded: Uint128, own: Uint128, cap: &CapRule) -> Uint128 {
    let others = bonded.saturating_sub(own);
    others
        .multiply_ratio(cap.numerator, cap.denominator)
        .min(cap.ceiling)
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
        // 50M bonded, nothing held: a fifth is 10M, the plan's genesis figure.
        assert_eq!(
            cap_total(noah(50_000_000), Uint128::zero(), &cap()),
            noah(10_000_000)
        );
        // The founder's seat counts: 294M bonded with 30M own leaves 264M to others.
        assert_eq!(
            cap_total(noah(294_000_000), noah(30_000_000), &cap()),
            noah(52_800_000)
        );
        // The ceiling binds once others hold 300M.
        assert_eq!(
            cap_total(noah(400_000_000), noah(10_000_000), &cap()),
            noah(60_000_000)
        );
        // own above bonded saturates to zero rather than underflowing.
        assert_eq!(cap_total(noah(1), noah(2), &cap()), Uint128::zero());
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
            a = cap_total(bonded, a, &cap());
            let bonded = base + a + b;
            b = cap_total(bonded, b, &cap());
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
}
