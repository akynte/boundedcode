import type { Product } from './types.ts';

// cheaperThan lists the products that cost less than limitCents, cheapest first.
export function cheaperThan(products: Product[], limitCents: number): Product[] {
  return products.filter((p) => p.priceCents < limitCents).sort((a, b) => a.priceCents - b.priceCents);
}
