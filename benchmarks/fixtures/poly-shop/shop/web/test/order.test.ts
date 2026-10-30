import test from 'node:test';
import assert from 'node:assert';
import { formatTotal, itemCount, parseOrder } from '../src/order.ts';

const json = '{"id":"o1","items":[{"sku":"A","qty":2},{"sku":"B","qty":1}],"total_cents":1234}';

test('parse and count', () => {
  const o = parseOrder(json);
  assert.strictEqual(itemCount(o), 3);
});

test('format', () => {
  assert.strictEqual(formatTotal(parseOrder(json)), '€12.34');
});

import { orderErrors } from '../src/validate.ts';

test('schema checks', () => {
  assert.deepStrictEqual(orderErrors({ id: 'o1', items: [], total_cents: 1 }), []);
  assert.deepStrictEqual(orderErrors({ id: 'o1', items: [], total_cents: '1' }), ['total_cents is not integer']);
});
