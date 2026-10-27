export interface Line {
  sku: string;
  priceCents: number;
  qty: number;
}

// Discount codes and their percentage off the subtotal.
const DISCOUNTS: Record<string, number> = { SAVE10: 10, SAVE25: 25 };

export class Cart {
  private lines: Line[] = [];
  private discountPercent = 0;

  add(sku: string, priceCents: number, qty = 1): void {
    if (qty <= 0) throw new Error("quantity must be positive");
    const line = this.lines.find((l) => l.sku === sku);
    if (line) line.qty += qty;
    else this.lines.push({ sku, priceCents, qty });
  }

  // applyCode sets the cart's discount; codes are case-insensitive.
  applyCode(code: string): void {
    const pct = DISCOUNTS[code.toUpperCase()];
    if (pct === undefined) throw new Error(`unknown code ${code}`);
    this.discountPercent = pct;
  }

  subtotal(): number {
    return this.lines.reduce((sum, l) => sum + l.priceCents * l.qty, 0);
  }

  // total is the subtotal less the discount; the discount is rounded down
  // to whole cents.
  total(): number {
    const sub = this.subtotal();
    return sub - Math.floor((sub * this.discountPercent) / 100);
  }
}
