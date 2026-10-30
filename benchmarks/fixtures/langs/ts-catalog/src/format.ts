import type { Product } from './types.ts';

// formatPrice shows a product's price in euros, e.g. "€12.50".
export function formatPrice(p: Product): string {
  return '€' + (p.priceCents / 100).toFixed(2);
}

// roundPrice rounds a price in cents to a multiple of step cents: down for
// 'down', up for 'up', and to the nearest (halves up) for 'nearest'.
export function roundPrice(cents: number, step: number, mode: 'down' | 'up' | 'nearest'): number {
  const q = cents / step;
  switch (mode) {
    case 'down':
      return Math.floor(q) * step;
    case 'up':
      return Math.floor(q) * step;
    case 'nearest':
      return Math.round(q) * step;
  }
}
