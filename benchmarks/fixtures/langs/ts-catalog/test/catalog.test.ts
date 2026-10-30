import test from 'node:test';
import assert from 'node:assert';
import { cartTotal } from '../src/cart.ts';
import { formatPrice, roundPrice } from '../src/format.ts';
import { cheaperThan } from '../src/search.ts';

const pen = { sku: 'P1', name: 'pen', priceCents: 250 };
const book = { sku: 'B1', name: 'book', priceCents: 1299 };

test('cart total', () => {
  assert.strictEqual(cartTotal([{ product: pen, quantity: 2 }, { product: book, quantity: 1 }]), 1799);
});

test('format', () => {
  assert.strictEqual(formatPrice(book), '€12.99');
});

test('cheaper than', () => {
  assert.deepStrictEqual(cheaperThan([book, pen], 2000).map((p) => p.sku), ['P1', 'B1']);
});

test('round down', () => {
  assert.strictEqual(roundPrice(1299, 50, 'down'), 1250);
});

test('round nearest', () => {
  assert.strictEqual(roundPrice(1225, 50, 'nearest'), 1250);
  assert.strictEqual(roundPrice(1224, 50, 'nearest'), 1200);
});

test('round up', () => {
  assert.strictEqual(roundPrice(1201, 50, 'up'), 1250);
  assert.strictEqual(roundPrice(1250, 50, 'up'), 1250);
});
