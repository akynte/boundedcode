package shop;

public final class Order {
    private final int cents;

    public Order(int cents) {
        this.cents = cents;
    }

    public int withTax(int percent) {
        return cents + cents * percent / 100;
    }
}
