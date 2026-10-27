package orders;

/** An order and its total in cents. */
public record Order(String id, String customer, long totalCents, Status status) {
    public enum Status { OPEN, PAID, CANCELLED }
}
