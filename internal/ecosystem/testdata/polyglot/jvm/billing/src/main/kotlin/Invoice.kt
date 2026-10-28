data class Invoice(val cents: Long)

fun total(invoices: List<Invoice>): Long = invoices.sumOf { it.cents }

object Currency {
    const val CODE = "EUR"
}
