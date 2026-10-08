import { createPromiseClient } from "@connectrpc/connect";
import { PaymentService } from "./gen/payments/v1/payment_connect";

const client = createPromiseClient(PaymentService, transport);

export async function pay(accountId: string) {
  return client.createPayment({ accountId, amountCents: 100n });
}

export async function refund(id: string) {
  return fetch(`/api/v1/refunds/${id}`);
}

export async function payOverHTTP(base: string) {
  return fetch(`${base}/v1/payments`, { method: "POST" });
}
