import { test } from "node:test";
import assert from "node:assert/strict";
import { RequestEpoch, Submission } from "../src/request-state.mjs";
test("old tenant response cannot replace current scope", () => {
  const s = new RequestEpoch();
  const a = s.next();
  const b = s.next();
  assert.equal(s.current(a), false);
  assert.equal(s.current(b), true);
});
test("uncertain retry preserves exact request and key", () => {
  const s = new Submission();
  const input = { items: [{ sku: "A", quantity: 1 }] };
  const a = s.prepare(input, "same-key");
  input.items[0].quantity = 99;
  const b = s.prepare({ items: [] }, "new-key");
  assert.equal(a, b);
  assert.equal(b.key, "same-key");
  assert.equal(b.payload.items[0].quantity, 1);
});
test("a resolved request allows a new submission", () => {
  const s = new Submission();
  s.prepare({ a: 1 }, "first");
  s.clear();
  assert.equal(s.prepare({ a: 2 }, "second").key, "second");
});
