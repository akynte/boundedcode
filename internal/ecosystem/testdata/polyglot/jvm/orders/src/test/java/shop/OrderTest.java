package shop;

import static org.junit.Assert.assertEquals;

import org.junit.Test;

public class OrderTest {
    @Test
    public void addsTax() {
        assertEquals(110, new Order(100).withTax(10));
    }
}
