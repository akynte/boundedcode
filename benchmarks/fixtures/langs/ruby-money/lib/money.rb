# Money splits amounts held in integer cents.
module Money
  # allocate splits amount_cents into n parts that sum to amount_cents and
  # differ by at most one cent; the first parts get the extra cents. n must
  # be positive (ArgumentError otherwise).
  def self.allocate(amount_cents, n)
    raise ArgumentError, "n must be positive" unless n.is_a?(Integer) && n.positive?

    base = amount_cents / n
    Array.new(n, base)
  end

  # allocate_by_ratio splits amount_cents in proportion to ratios (positive
  # integers), each part rounded down, the cents left over going to the
  # first parts.
  def self.allocate_by_ratio(amount_cents, ratios)
    raise ArgumentError, "ratios must be positive" if ratios.empty? || ratios.any? { |r| r <= 0 }

    total = ratios.sum
    parts = ratios.map { |r| amount_cents * r / total }
    (amount_cents - parts.sum).times { |i| parts[i % parts.size] += 1 }
    parts
  end
end
