import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import * as path from 'node:path';
import { mkdtemp, rm, mkdir, cp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import * as vm from 'node:vm';

const execFileAsync = promisify(execFile);

test('build-webview-assets: child process exits with code 1 and preserves existing bundles on any missing required input (§4.7 P2)', async () => {
  const bundlerScript = path.resolve(__dirname, '../../tools/build-webview-assets.mjs');
  const realRoot = path.resolve(__dirname, '../..');

  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-build-assets-test-'));

  try {
    const tempCoreDir = path.join(tempDir, 'out', 'core');
    const tempWebDir = path.join(tempDir, 'out', 'web');
    const tempSrcWebDir = path.join(tempDir, 'src', 'web');
    await mkdir(tempCoreDir, { recursive: true });
    await mkdir(tempWebDir, { recursive: true });
    await mkdir(tempSrcWebDir, { recursive: true });

    const requiredFiles = [
      { name: 'out/core/answer_state.js', realPath: path.join(realRoot, 'out', 'core', 'answer_state.js'), tempPath: path.join(tempCoreDir, 'answer_state.js') },
      { name: 'out/core/recovery_state.js', realPath: path.join(realRoot, 'out', 'core', 'recovery_state.js'), tempPath: path.join(tempCoreDir, 'recovery_state.js') },
      { name: 'src/web/dom_interaction.ts', realPath: path.join(realRoot, 'src', 'web', 'dom_interaction.ts'), tempPath: path.join(tempSrcWebDir, 'dom_interaction.ts') },
      { name: 'src/web/recovery_view.ts', realPath: path.join(realRoot, 'src', 'web', 'recovery_view.ts'), tempPath: path.join(tempSrcWebDir, 'recovery_view.ts') },
      { name: 'src/web/recovery_controller.ts', realPath: path.join(realRoot, 'src', 'web', 'recovery_controller.ts'), tempPath: path.join(tempSrcWebDir, 'recovery_controller.ts') },
      { name: 'src/web/chat_adapter.ts', realPath: path.join(realRoot, 'src', 'web', 'chat_adapter.ts'), tempPath: path.join(tempSrcWebDir, 'chat_adapter.ts') },
    ];

    // Copy all 6 compiled files to isolated tempDir
    for (const file of requiredFiles) {
      await cp(file.realPath, file.tempPath);
    }

    const dstAnswerState = path.join(tempWebDir, 'answer_state.js');
    const dstAdapterBundle = path.join(tempWebDir, 'chat_adapter.bundle.js');

    const SENTINEL_ANSWER = '// PRE-EXISTING_ANSWER_STATE_SENTINEL';
    const SENTINEL_ADAPTER = '// PRE-EXISTING_ADAPTER_BUNDLE_SENTINEL';

    // Set sentinel contents on destination files to verify they are untouched on error
    await writeFile(dstAnswerState, SENTINEL_ANSWER, 'utf8');
    await writeFile(dstAdapterBundle, SENTINEL_ADAPTER, 'utf8');

    // 1. Missing each required input one by one -> must exit 1, output error and hint, and leave outputs untouched
    for (const file of requiredFiles) {
      const backupContent = await readFile(file.tempPath, 'utf8');
      await rm(file.tempPath);

      try {
        await execFileAsync(process.execPath, [bundlerScript, `--root=${tempDir}`]);
        assert.fail(`Expected bundler to fail when ${file.name} is missing`);
      } catch (err: any) {
        assert.equal(err.code, 1, `Process must exit with code 1 when ${file.name} is missing`);
        assert.ok(err.stderr.includes(file.name), `stderr must mention missing file ${file.name}`);
        assert.ok(err.stderr.includes("Run 'tsc -p .' first."), `stderr must include rebuild hint`);

        // Assert existing output files were not touched or overwritten (§4.7 P2)
        const currentAnswer = await readFile(dstAnswerState, 'utf8');
        const currentAdapter = await readFile(dstAdapterBundle, 'utf8');
        assert.equal(currentAnswer, SENTINEL_ANSWER, `dst answer_state.js must remain untouched on missing ${file.name}`);
        assert.equal(currentAdapter, SENTINEL_ADAPTER, `dst chat_adapter.bundle.js must remain untouched on missing ${file.name}`);
      }

      // Restore file before testing next
      await writeFile(file.tempPath, backupContent, 'utf8');
    }

    // 2. Success case: All 6 files present -> must exit 0 and produce working bundles
    const result = await execFileAsync(process.execPath, [bundlerScript, `--root=${tempDir}`]);
    assert.equal(result.stderr, '');

    const generatedAnswerState = await readFile(dstAnswerState, 'utf8');
    const generatedAdapterBundle = await readFile(dstAdapterBundle, 'utf8');

    assert.notEqual(generatedAnswerState, SENTINEL_ANSWER);
    assert.notEqual(generatedAdapterBundle, SENTINEL_ADAPTER);

    // Verify answer_state.js execution in sandboxed vm context
    const answerSandbox: any = { window: {} };
    answerSandbox.window = answerSandbox;
    vm.createContext(answerSandbox);
    vm.runInContext(generatedAnswerState, answerSandbox);

    assert.equal(typeof answerSandbox.createAnswerState, 'function');
    assert.equal(typeof answerSandbox.createRecoveryState, 'function');
    const stateManager = answerSandbox.createAnswerState();
    assert.ok(stateManager);
    const recManager = stateManager.getRecoveryState();
    assert.ok(recManager);
    assert.equal(typeof recManager.listItems, 'function');

    // Verify chat_adapter.bundle.js execution in sandboxed vm context
    const adapterSandbox: any = {
      window: {
        document: {
          createElement: () => ({
            append: () => {},
            remove: () => {},
            addEventListener: () => {},
            removeEventListener: () => {},
            classList: { contains: () => false },
          }),
        },
      },
    };
    adapterSandbox.window.window = adapterSandbox.window;
    vm.createContext(adapterSandbox);
    vm.runInContext(generatedAdapterBundle, adapterSandbox);

    assert.equal(typeof adapterSandbox.createWebviewActionAdapter, 'function');
    assert.equal(typeof adapterSandbox.createWebviewInputAdapter, 'function');
    assert.equal(typeof adapterSandbox.createSuggestController, 'function');
    assert.equal(typeof adapterSandbox.createWebviewReceiveHandlers, 'function');
    assert.equal(typeof adapterSandbox.createWebviewRecoveryController, 'function');
    assert.equal(typeof adapterSandbox.createRecoveryView, 'function');
    assert.equal(typeof adapterSandbox.captureSelection, 'function');
    assert.equal(typeof adapterSandbox.restoreSelection, 'function');
    assert.equal(typeof adapterSandbox.restoreFocus, 'function');
    assert.equal(typeof adapterSandbox.moveDomChild, 'function');
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
