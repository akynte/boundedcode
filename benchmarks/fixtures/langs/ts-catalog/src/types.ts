// Product is an item of the catalog. Prices are integer cents.
export interface Product {
  sku: string;
  name: string;
  priceCents: number;
}

export interface CartLine {
  product: Product;
  quantity: number;
}
