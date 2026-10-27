import { test } from "node:test";
import assert from "node:assert/strict";
import { Cart } from "../src/cart.ts";
import { formatCents } from "../src/format.ts";

test("subtotal adds lines", () => {
  const c = new Cart();
  c.add("a", 250, 2);
  c.add("b", 100);
  c.add("a", 250);
  assert.equal(c.subtotal(), 850);
});

test("unknown code is refused", () => {
  assert.throws(() => new Cart().applyCode("NOPE"));
});

test("formatCents", () => {
  assert.equal(formatCents(1234), "12.34");
  assert.equal(formatCents(5), "0.05");
});
