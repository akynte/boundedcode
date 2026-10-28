class Money {
  final int cents;
  const Money(this.cents);

  Money operator +(Money o) => Money(cents + o.cents);
}

int centsOf(Money m) {
  return m.cents;
}
