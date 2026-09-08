import * as path from 'path';
import { runTests } from '@vscode/test-electron';

/**
 * Start a real VS Code with this extension in it and run the self-check.
 *
 * The unit tests prove the rules; only this can prove the manifest. A typo in `contributes` passes
 * every unit test and then does nothing in the editor — no error, because a view nobody registered
 * simply never appears.
 */
async function main(): Promise<void> {
  const root = path.resolve(__dirname, '..', '..');
  await runTests({
    extensionDevelopmentPath: root,
    extensionTestsPath: path.resolve(__dirname, 'index'),
    // The repository itself as the workspace: it has a live companion, so the socket check has
    // something true to check against.
    launchArgs: [path.resolve(root, '..', '..'), '--disable-extensions=false', '--disable-gpu'],
    extensionTestsEnv: { MAGI_VSCODE_SELFCHECK: process.env.MAGI_VSCODE_SELFCHECK ?? '' },
  });
}

main().catch((e) => { console.error(e instanceof Error ? e.message : e); process.exit(1); });
