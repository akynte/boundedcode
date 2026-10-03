import { createPayment } from "./payments";

// checkout is the public entry point that charges a cart.
export async function checkout(accountId: string, totalCents: number): Promise<string> {
  const payment = await createPayment({ account_id: accountId, amount_cents: totalCents, currency: "EUR" });
  return payment.id;
}
