// The order summary as the API sends it (contracts/summary.schema.json),
// in the client's own field names.
export interface OrderSummary {
  id: string;
  subtotalCents: number;
  totalCents: number;
}

// parseSummary reads the API's JSON (snake_case) into an OrderSummary.
export function parseSummary(json: string): OrderSummary {
  const raw = JSON.parse(json);
  return { id: raw.id, subtotalCents: raw.subtotal_cents, totalCents: raw.total_cents };
}

// describe is the one-line text shown in the order list: "o1: 5.00".
export function describe(s: OrderSummary): string {
  return `${s.id}: ${(s.totalCents / 100).toFixed(2)}`;
}
