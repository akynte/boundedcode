//! Account balances in cents.

use std::collections::HashMap;
use std::fmt;

#[derive(Debug, PartialEq, Eq)]
pub enum LedgerError {
    /// The amount was zero or negative.
    NonPositiveAmount,
    /// The source account does not hold the amount.
    InsufficientFunds { account: String, balance: i64, needed: i64 },
}

impl fmt::Display for LedgerError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LedgerError::NonPositiveAmount => write!(f, "amount must be positive"),
            LedgerError::InsufficientFunds { account, balance, needed } => {
                write!(f, "{account}: balance {balance}, needed {needed}")
            }
        }
    }
}

impl std::error::Error for LedgerError {}

#[derive(Debug, Default)]
pub struct Ledger {
    balances: HashMap<String, i64>,
}

impl Ledger {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn deposit(&mut self, account: &str, cents: i64) -> Result<(), LedgerError> {
        if cents <= 0 {
            return Err(LedgerError::NonPositiveAmount);
        }
        *self.balances.entry(account.to_string()).or_insert(0) += cents;
        Ok(())
    }

    /// The account's balance; an account never used has a balance of 0.
    pub fn balance(&self, account: &str) -> i64 {
        self.balances.get(account).copied().unwrap_or(0)
    }

    /// Moves cents from one account to another, all or nothing.
    pub fn transfer(&mut self, from: &str, to: &str, cents: i64) -> Result<(), LedgerError> {
        if cents <= 0 {
            return Err(LedgerError::NonPositiveAmount);
        }
        let balance = self.balance(from);
        if balance < cents {
            return Err(LedgerError::InsufficientFunds { account: from.to_string(), balance, needed: cents });
        }
        *self.balances.entry(from.to_string()).or_insert(0) -= cents;
        *self.balances.entry(to.to_string()).or_insert(0) += cents;
        Ok(())
    }
}
