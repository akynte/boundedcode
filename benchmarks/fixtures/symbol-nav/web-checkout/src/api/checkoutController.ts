import { submitPayment } from "../services/paymentService";
import type { PaymentGateway, PaymentRequest } from "../types/payment";

// CheckoutController is the API component behind POST /checkout.
export class CheckoutController {
  constructor(private readonly gateway: PaymentGateway) {}

  async handle(body: unknown): Promise<{ status: number; body: unknown }> {
    const req = body as PaymentRequest;
    try {
      const result = await submitPayment(this.gateway, req);
      return { status: 201, body: result };
    } catch (e) {
      return { status: 422, body: { error: (e as Error).message } };
    }
  }
}
