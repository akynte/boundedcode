package orders;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.NoSuchElementException;
import java.util.Optional;

/** The orders known to the shop, by id. */
public final class OrderBook {
    private final Map<String, Order> orders = new LinkedHashMap<>();

    public void add(Order order) {
        if (orders.putIfAbsent(order.id(), order) != null) {
            throw new IllegalArgumentException("duplicate order " + order.id());
        }
    }

    public Optional<Order> find(String id) {
        return Optional.ofNullable(orders.get(id));
    }

    /** The total of the customer's paid orders, in cents. */
    public long paidTotal(String customer) {
        long sum = 0;
        for (Order o : orders.values()) {
            if (o.customer().equals(customer) && o.status() == Order.Status.PAID) {
                sum += o.totalCents();
            }
        }
        return sum;
    }

    /** Cancels an open order; a paid order cannot be cancelled. */
    public Order cancel(String id) {
        Order o = orders.get(id);
        if (o == null) {
            throw new NoSuchElementException("no order " + id);
        }
        if (o.status() == Order.Status.PAID) {
            throw new IllegalStateException("order " + id + " is paid");
        }
        Order cancelled = new Order(o.id(), o.customer(), o.totalCents(), Order.Status.CANCELLED);
        orders.put(id, cancelled);
        return cancelled;
    }
}
