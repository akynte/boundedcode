import type { PaymentGateway, PaymentRequest, PaymentResult } from "../types/payment";

// validateRequest rejects malformed requests before they reach a gateway.
export function validateRequest(req: PaymentRequest): string | null {
  if (!req.accountId) return "accountId is required";
  if (req.amountCents <= 0) return "amountCents must be positive";
  if (req.currency.length !== 3) return "currency must be a 3-letter code";
  return null;
}

// submitPayment validates and charges a payment.
export async function submitPayment(gateway: PaymentGateway, req: PaymentRequest): Promise<PaymentResult> {
  const problem = validateRequest(req);
  if (problem) {
    throw new Error(problem);
  }
  return gateway.charge(req);
}
