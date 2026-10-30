'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { lineTotal, invoiceTotal } = require('../src/invoice');

test('line total', () => {
  assert.strictEqual(lineTotal({ unitPrice: 5, quantity: 3 }), 15);
});

test('whole amounts', () => {
  assert.strictEqual(invoiceTotal([{ unitPrice: 10, quantity: 2 }], 0.5), 30);
});
