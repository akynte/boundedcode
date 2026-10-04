// Shared payment types used by the API layer, services and gateways.

export interface PaymentRequest {
  accountId: string;
  amountCents: number;
  currency: string;
}

export interface PaymentResult {
  id: string;
  status: "pending" | "failed";
}

// PaymentGateway charges a payment with an external provider.
export interface PaymentGateway {
  charge(req: PaymentRequest): Promise<PaymentResult>;
}
