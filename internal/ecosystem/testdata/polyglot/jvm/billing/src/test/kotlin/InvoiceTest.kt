import kotlin.test.Test
import kotlin.test.assertEquals

class InvoiceTest {
    @Test
    fun sums() {
        assertEquals(3L, total(listOf(Invoice(1), Invoice(2))))
    }
}
