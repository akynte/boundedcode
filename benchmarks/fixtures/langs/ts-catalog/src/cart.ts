import type { CartLine } from './types.ts';

// cartTotal is the cart's total in cents.
export function cartTotal(lines: CartLine[]): number {
  return lines.reduce((sum, l) => sum + l.product.priceCents * l.quantity, 0);
}
