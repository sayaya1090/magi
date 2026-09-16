import * as fc from 'fast-check';

/**
 * Builds a safe, shell-quoted node --test command to reproduce a property failure.
 * Quotes and escapes regex characters and shell metacharacters in propertyName so
 * property names containing quotes, braces, colons, or Korean text match exactly.
 */
export function buildReproductionCommand(
  propertyName: string,
  seed: number,
  path: string | null | undefined
): string {
  const escapedPattern = propertyName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const safeQuotedPattern = `'^${escapedPattern.replace(/'/g, "'\\''")}$'`;
  const seedPart = `MAGI_FC_SEED=${seed}`;
  const pathPart = path ? ` MAGI_FC_PATH='${path.replace(/'/g, "'\\''")}'` : '';
  return `${seedPart}${pathPart} node --test --test-name-pattern=${safeQuotedPattern} clients/vscode/out/test/*.property.test.js`;
}

/**
 * Parses and validates fast-check runner parameters from environment variables:
 * - MAGI_FC_SEED: 32-bit signed integer numeric seed for reproducing a property failure.
 *   Rejects partial parsing (e.g. "12junk"), decimals ("1.5"), scientific notation ("1e3"),
 *   and integers outside [-2147483648, 2147483647].
 * - MAGI_FC_PATH: colon-separated non-negative integer indices (e.g. "0:1:2").
 *   Rejects non-integer, empty segments, negative numbers, and usage without MAGI_FC_SEED.
 */
export function getFcParameters(defaultNumRuns = 100): fc.Parameters<unknown> {
  let seed: number | undefined = undefined;
  if (process.env.MAGI_FC_SEED !== undefined && process.env.MAGI_FC_SEED !== '') {
    const raw = process.env.MAGI_FC_SEED.trim();
    if (!/^[-+]?\d+$/.test(raw)) {
      throw new Error(
        `Invalid MAGI_FC_SEED environment variable: "${process.env.MAGI_FC_SEED}". Value must be a valid integer without decimals, exponents, or trailing text.`
      );
    }
    const parsed = Number(raw);
    if (!Number.isSafeInteger(parsed) || parsed < -2147483648 || parsed > 2147483647) {
      throw new Error(
        `Invalid MAGI_FC_SEED environment variable: "${process.env.MAGI_FC_SEED}". Value must be a 32-bit signed integer between -2147483648 and 2147483647.`
      );
    }
    seed = parsed;
  }

  let path: string | undefined = undefined;
  if (process.env.MAGI_FC_PATH !== undefined && process.env.MAGI_FC_PATH !== '') {
    if (seed === undefined) {
      throw new Error(
        `MAGI_FC_PATH environment variable requires MAGI_FC_SEED to be set.`
      );
    }
    const rawPath = process.env.MAGI_FC_PATH.trim();
    if (!/^(\d+)(:\d+)*$/.test(rawPath)) {
      throw new Error(
        `Invalid MAGI_FC_PATH environment variable: "${process.env.MAGI_FC_PATH}". Path must be a colon-separated sequence of non-negative integer indices (e.g. "0:1:2").`
      );
    }
    path = rawPath;
  }

  return {
    numRuns: defaultNumRuns,
    seed,
    path,
    endOnFailure: false, // Enable full counterexample shrinking
  };
}

/**
 * Executes a fast-check property with timing measurement, counterexample shrinking,
 * and structured failure diagnostics using fc.check.
 * On failure, prints the property name, seed, path, shrunk counterexample, shrink count,
 * and a shell-safe reproduction command, then throws the underlying error to fail the test.
 */
export function assertProperty<Ts extends [unknown, ...unknown[]]>(
  propertyName: string,
  property: fc.IProperty<Ts>,
  params?: fc.Parameters<unknown>
): void {
  const p = params || getFcParameters();
  const start = Date.now();

  const details = fc.check(property, p);
  const elapsedMs = Date.now() - start;

  if (details.failed) {
    const seed = details.seed;
    const path = details.counterexamplePath;
    const reproCmd = buildReproductionCommand(propertyName, seed, path);

    console.error(`\n================================================================`);
    console.error(`[Fast-Check Property Failure] ${propertyName}`);
    console.error(`================================================================`);
    console.error(`  Failure Run     : test ${details.numRuns} of ${p.numRuns || 100}`);
    console.error(`  Shrink Count    : ${details.numShrinks} shrink(s)`);
    console.error(`  Reproduction    : MAGI_FC_SEED=${seed}${path ? ` MAGI_FC_PATH="${path}"` : ''}`);
    console.error(`  Counterexample  : ${JSON.stringify(details.counterexample)}`);
    console.error(`----------------------------------------------------------------`);
    console.error(`  To reproduce only this property failure in 1 run:`);
    console.error(`  ${reproCmd}`);
    console.error(`================================================================\n`);

    const err =
      details.errorInstance instanceof Error
        ? details.errorInstance
        : new Error(
            `Fast-check property "${propertyName}" failed after ${details.numRuns} tests (${details.numShrinks} shrinks)`
          );
    (err as any).seed = seed;
    (err as any).path = path;
    (err as any).counterexample = details.counterexample;
    (err as any).numShrinks = details.numShrinks;
    throw err;
  }

  if (process.env.MAGI_FC_VERBOSE) {
    console.log(`  [fc] "${propertyName}" completed in ${elapsedMs}ms (${details.numRuns} runs)`);
  }
}

/**
 * Shared generators for property tests: Korean text, whitespace, newlines, colons in IDs.
 */
export const arbKoreanOrAsciiString = fc.oneof(
  fc.constant(''),
  fc.constant('   '),
  fc.constant('\n\n'),
  fc.constant('  앞뒤 공백과 개행 \n  '),
  fc.constant('프로덕션 환경 배포'),
  fc.constant('TypeScript 리팩토링 검증'),
  fc.constant('user-profile.ts'),
  fc.string({ minLength: 1, maxLength: 60 }),
  fc.string({ minLength: 1, maxLength: 40 })
);

export const arbBlankString = fc.constantFrom('', '   ', '\t\t', '\n\n', '  \t \n ');

export const arbNonBlankString = fc
  .string({ minLength: 1, maxLength: 60 })
  .map((s) => (s.trim().length > 0 ? s : s + 'valid_payload'));

