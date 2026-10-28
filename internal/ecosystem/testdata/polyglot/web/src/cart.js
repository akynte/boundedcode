'use strict';

function applyDiscount(total, percent) {
  return Math.round(total * (100 - percent) / 100);
}

class Basket {
  constructor() {
    this.items = [];
  }

  add(price) {
    this.items.push(price);
  }
}

const totalOf = (basket) => basket.items.reduce((a, b) => a + b, 0);

module.exports = { applyDiscount, Basket, totalOf };
