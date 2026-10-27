import { test } from "node:test";
import assert from "node:assert/strict";
import { describe, parseSummary } from "../src/summary.ts";

test("parseSummary maps the API's names", () => {
  const s = parseSummary('{"id":"o1","subtotal_cents":600,"total_cents":500}');
  assert.deepEqual(s, { id: "o1", subtotalCents: 600, totalCents: 500 });
});

test("describe", () => {
  assert.equal(describe({ id: "o1", subtotalCents: 600, totalCents: 500 }), "o1: 5.00");
});
