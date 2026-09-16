import { test } from 'node:test';
import * as fc from 'fast-check';
import { assertProperty } from '../support/fc_helpers';

// Fixture Property A: Passing property with complex name
test('fixture: [pass] "quoted" {braced} (paren) 정상 통과 속성', () => {
  assertProperty(
    'fixture: [pass] "quoted" {braced} (paren) 정상 통과 속성',
    fc.property(fc.integer(), (n) => typeof n === 'number')
  );
});

// Fixture Property B: Intentionally failing property with complex name
// Fails for n >= 10, shrinks to minimal counterexample 10
test('fixture: [fail] "error: empty" {key: "val"} (special) 의도적 실패 속성', () => {
  assertProperty(
    'fixture: [fail] "error: empty" {key: "val"} (special) 의도적 실패 속성',
    fc.property(fc.integer({ min: 0, max: 100000 }), (n) => n < 10)
  );
});
