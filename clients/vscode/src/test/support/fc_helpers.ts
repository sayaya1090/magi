import * as fc from 'fast-check';

/**
 * Parses and validates fast-check runner parameters from environment variables:
 * - MAGI_FC_SEED: numeric seed for reproducing a specific property test failure.
 * - MAGI_FC_PATH: string path representing the replay path within the shrink tree.
 *
 * Throws explicit descriptive error if MAGI_FC_SEED is provided but not a valid integer.
 */
export function getFcParameters(defaultNumRuns = 100): fc.Parameters<unknown> {
  let seed: number | undefined = undefined;
  if (process.env.MAGI_FC_SEED !== undefined && process.env.MAGI_FC_SEED !== '') {
    const parsed = parseInt(process.env.MAGI_FC_SEED, 10);
    if (Number.isNaN(parsed)) {
      throw new Error(
        `Invalid MAGI_FC_SEED environment variable: "${process.env.MAGI_FC_SEED}". Value must be a valid integer.`
      );
    }
    seed = parsed;
  }

  const path = process.env.MAGI_FC_PATH || undefined;

  return {
    numRuns: defaultNumRuns,
    seed,
    path,
    endOnFailure: true,
  };
}

/**
 * Executes a fast-check property with timing measurement and enhanced failure diagnostics.
 * On failure, prints the property name, MAGI_FC_SEED, MAGI_FC_PATH, and minimal reproduction command.
 */
export function assertProperty<Ts extends [unknown, ...unknown[]]>(
  propertyName: string,
  property: fc.IProperty<Ts>,
  params?: fc.Parameters<unknown>
): void {
  const p = params || getFcParameters();
  const start = Date.now();

  try {
    fc.assert(property, p);
  } catch (err: any) {
    const seedMatch = err.message?.match(/seed:\s*(-?\d+)/);
    const pathMatch = err.message?.match(/path:\s*"([^"]+)"/);
    const seed = err.seed !== undefined ? err.seed : (seedMatch ? seedMatch[1] : undefined);
    const path = err.path !== undefined ? err.path : (pathMatch ? pathMatch[1] : undefined);

    console.error(`\n================================================================`);
    console.error(`[Fast-Check Property Failure] ${propertyName}`);
    console.error(`================================================================`);
    if (seed !== undefined) {
      console.error(`  Reproduction Seed : MAGI_FC_SEED=${seed}`);
    }
    if (path !== undefined) {
      console.error(`  Reproduction Path : MAGI_FC_PATH="${path}"`);
    }
    console.error(`  To reproduce only this property test:`);
    console.error(
      `  MAGI_FC_SEED=${seed ?? ''} MAGI_FC_PATH="${path ?? ''}" node --test --test-name-pattern="${propertyName}" clients/vscode/out/test/*.property.test.js`
    );
    console.error(`================================================================\n`);
    throw err;
  } finally {
    const elapsedMs = Date.now() - start;
    if (process.env.MAGI_FC_VERBOSE) {
      console.log(`  [fc] "${propertyName}" completed in ${elapsedMs}ms (${p.numRuns} runs)`);
    }
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

