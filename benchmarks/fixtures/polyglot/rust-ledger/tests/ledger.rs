use ledger::{Ledger, LedgerError};

#[test]
fn deposit_and_balance() {
    let mut l = Ledger::new();
    l.deposit("a", 500).unwrap();
    l.deposit("a", 250).unwrap();
    assert_eq!(l.balance("a"), 750);
    assert_eq!(l.balance("nobody"), 0);
}

#[test]
fn deposit_must_be_positive() {
    assert_eq!(Ledger::new().deposit("a", 0), Err(LedgerError::NonPositiveAmount));
}
