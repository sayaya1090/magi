import * as path from 'path';
import * as fs from 'fs';

/**
 * The mocha-shaped entry point `@vscode/test-electron` calls INSIDE a running VS Code.
 *
 * There is no mocha here. The runner only needs an exported `run()` that resolves when the checks
 * pass and rejects when they do not — and one function is easier to keep honest than a framework
 * whose reporter can swallow a failure.
 */
export async function run(): Promise<void> {
  const { selfCheck } = await import('./selfcheck');
  const fail = await selfCheck();
  const where = process.env.MAGI_VSCODE_SELFCHECK;
  if (where) fs.writeFileSync(where, fail.length ? 'FAIL\n' + fail.join('\n') : 'OK');
  if (fail.length) throw new Error('the extension is not what its manifest promises:\n  ' + fail.join('\n  '));
  void path;
}
