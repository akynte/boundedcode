'use strict';

// lineTotal is a line's amount: unit price times quantity.
function lineTotal(line) {
  return line.unitPrice * line.quantity;
}

// invoiceTotal is the invoice's total with tax: the lines' sum plus tax at
// rate (0.2 for 20%). Amounts are in currency units (euros), and the total
// must be exact to the cent: 10.01, never 10.010000000000002.
function invoiceTotal(lines, rate) {
  const net = lines.reduce((sum, l) => sum + lineTotal(l), 0);
  return net + net * rate;
}

module.exports = { lineTotal, invoiceTotal };
