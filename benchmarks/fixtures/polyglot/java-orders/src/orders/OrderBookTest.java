package orders;

/** Tests without a framework: run with `make test`. */
public final class OrderBookTest {
    static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    public static void main(String[] args) {
        OrderBook book = new OrderBook();
        book.add(new Order("o1", "ann", 1200, Order.Status.OPEN));
        check(book.find("o1").isPresent(), "find o1");
        check(book.find("o9").isEmpty(), "o9 is unknown");
        try {
            book.add(new Order("o1", "bob", 5, Order.Status.OPEN));
            throw new AssertionError("a duplicate id must be refused");
        } catch (IllegalArgumentException expected) {
            // refused
        }
        check(book.cancel("o1").status() == Order.Status.CANCELLED, "cancel o1");
        System.out.println("OrderBookTest: ok");
    }
}
