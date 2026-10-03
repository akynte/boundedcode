// Client for payment-service's HTTP API (POST /v1/payments).
export interface CreatePaymentRequest {
  account_id: string;
  amount_cents: number;
  currency: string;
}

export interface Payment {
  id: string;
  account_id: string;
  amount_cents: number;
  currency: string;
  status: string;
}

const PAYMENT_SERVICE_URL = "http://payment-service:8080";

export async function createPayment(req: CreatePaymentRequest): Promise<Payment> {
  const res = await fetch(`${PAYMENT_SERVICE_URL}/v1/payments`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    throw new Error(`createPayment failed: ${res.status}`);
  }
  return (await res.json()) as Payment;
}
