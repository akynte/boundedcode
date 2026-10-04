import type { PaymentGateway, PaymentRequest, PaymentResult } from "../types/payment";

// CardGateway calls the card processor's HTTP API.
export class CardGateway implements PaymentGateway {
  constructor(private readonly baseUrl: string) {}

  async charge(req: PaymentRequest): Promise<PaymentResult> {
    const res = await fetch(`${this.baseUrl}/charges`, { method: "POST", body: JSON.stringify(req) });
    if (!res.ok) {
      return { id: "", status: "failed" };
    }
    return (await res.json()) as PaymentResult;
  }
}
