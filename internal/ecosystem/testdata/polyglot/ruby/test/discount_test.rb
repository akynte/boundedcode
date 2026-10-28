require "minitest/autorun"
require "discount"

class DiscountTest < Minitest::Test
  def test_apply
    assert_equal 90, Shop::Discount.new(10).apply(100)
  end
end
