'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { applyDiscount, Basket, totalOf } = require('./cart');

test('discount', () => {
  assert.strictEqual(applyDiscount(200, 10), 180);
  const b = new Basket();
  b.add(5);
  assert.strictEqual(totalOf(b), 5);
});
