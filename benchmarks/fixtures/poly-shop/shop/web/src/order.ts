import type { Order } from './client.gen.ts';

// itemCount is the number of units in an order.
export function itemCount(o: Order): number {
  return o.items.reduce((n, it) => n + it.qty, 0);
}

// formatTotal shows the order's total, e.g. "€12.34".
export function formatTotal(o: Order): string {
  return '€' + (o.total_cents / 100).toFixed(2);
}

// parseOrder reads an order from the server's JSON.
export function parseOrder(json: string): Order {
  const o = JSON.parse(json);
  return { id: o.id, items: o.items.map((it: { sku: string; qty: number }) => ({ sku: it.sku, qty: it.qty })), total_cents: o.total_cents };
}
