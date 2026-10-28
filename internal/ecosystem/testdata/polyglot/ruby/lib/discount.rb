module Shop
  class Discount
    def initialize(percent)
      @percent = percent
    end

    def apply(cents)
      cents * (100 - @percent) / 100
    end

    def self.none
      new(0)
    end
  end
end
