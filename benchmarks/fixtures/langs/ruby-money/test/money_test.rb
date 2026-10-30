require "minitest/autorun"
require "money"

class MoneyTest < Minitest::Test
  def test_even_split
    assert_equal [50, 50], Money.allocate(100, 2)
  end

  def test_ratio
    assert_equal [70, 30], Money.allocate_by_ratio(100, [7, 3])
  end

  def test_rejects_zero_parts
    assert_raises(ArgumentError) { Money.allocate(100, 0) }
  end
end
