import type { PaymentGateway, PaymentRequest, PaymentResult } from "../types/payment";

// MockGateway accepts every payment; used in tests and demos.
export class MockGateway implements PaymentGateway {
  private next = 1;

  async charge(_req: PaymentRequest): Promise<PaymentResult> {
    return { id: `mock-${this.next++}`, status: "pending" };
  }
}
