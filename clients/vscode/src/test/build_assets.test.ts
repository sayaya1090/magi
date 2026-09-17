import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import * as path from 'node:path';
import { existsSync } from 'node:fs';
import { mkdtemp, rm, mkdir, cp, readFile, writeFile, stat } from 'node:fs/promises';
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
    const tempSrcCoreDir = path.join(tempDir, 'src', 'core');
    const tempSrcWebDir = path.join(tempDir, 'src', 'web');
    await mkdir(tempCoreDir, { recursive: true });
    await mkdir(tempWebDir, { recursive: true });
    await mkdir(tempSrcCoreDir, { recursive: true });
    await mkdir(tempSrcWebDir, { recursive: true });

    const requiredFiles = [
      { name: 'out/core/answer_state.js', realPath: path.join(realRoot, 'out', 'core', 'answer_state.js'), tempPath: path.join(tempCoreDir, 'answer_state.js') },
      { name: 'out/core/recovery_state.js', realPath: path.join(realRoot, 'out', 'core', 'recovery_state.js'), tempPath: path.join(tempCoreDir, 'recovery_state.js') },
      { name: 'src/web/dom_interaction.ts', realPath: path.join(realRoot, 'src', 'web', 'dom_interaction.ts'), tempPath: path.join(tempSrcWebDir, 'dom_interaction.ts') },
      { name: 'src/web/recovery_view.ts', realPath: path.join(realRoot, 'src', 'web', 'recovery_view.ts'), tempPath: path.join(tempSrcWebDir, 'recovery_view.ts') },
      { name: 'src/web/recovery_controller.ts', realPath: path.join(realRoot, 'src', 'web', 'recovery_controller.ts'), tempPath: path.join(tempSrcWebDir, 'recovery_controller.ts') },
      { name: 'src/web/chat_adapter.ts', realPath: path.join(realRoot, 'src', 'web', 'chat_adapter.ts'), tempPath: path.join(tempSrcWebDir, 'chat_adapter.ts') },
      { name: 'src/web/markdown_render.ts', realPath: path.join(realRoot, 'src', 'web', 'markdown_render.ts'), tempPath: path.join(tempSrcWebDir, 'markdown_render.ts') },
      { name: 'src/core/webview_protocol.ts', realPath: path.join(realRoot, 'src', 'core', 'webview_protocol.ts'), tempPath: path.join(tempSrcCoreDir, 'webview_protocol.ts') },
    ];

    // Copy all 7 files to isolated tempDir
    for (const file of requiredFiles) {
      await cp(file.realPath, file.tempPath);
    }

    const dstAnswerState = path.join(tempWebDir, 'answer_state.js');
    const dstAdapterBundle = path.join(tempWebDir, 'chat_adapter.bundle.js');
    const dstMarkdownRender = path.join(tempWebDir, 'markdown_render.js');
    const dstProtocolBundle = path.join(tempCoreDir, 'webview_protocol.js');

    const SENTINEL_ANSWER = '// PRE-EXISTING_ANSWER_STATE_SENTINEL';
    const SENTINEL_ADAPTER = '// PRE-EXISTING_ADAPTER_BUNDLE_SENTINEL';
    const SENTINEL_MARKDOWN = '// PRE-EXISTING_MARKDOWN_RENDER_SENTINEL';
    const SENTINEL_PROTOCOL = '// PRE-EXISTING_PROTOCOL_BUNDLE_SENTINEL';

    // Set sentinel contents on destination files to verify they are untouched on error
    await writeFile(dstAnswerState, SENTINEL_ANSWER, 'utf8');
    await writeFile(dstAdapterBundle, SENTINEL_ADAPTER, 'utf8');
    await writeFile(dstMarkdownRender, SENTINEL_MARKDOWN, 'utf8');
    await writeFile(dstProtocolBundle, SENTINEL_PROTOCOL, 'utf8');

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

        // Assert existing output files were not touched or overwritten (§4.7 P2, §5.8.3, §5.8.5)
        const currentAnswer = await readFile(dstAnswerState, 'utf8');
        const currentAdapter = await readFile(dstAdapterBundle, 'utf8');
        const currentMarkdown = await readFile(dstMarkdownRender, 'utf8');
        const currentProtocol = await readFile(dstProtocolBundle, 'utf8');
        assert.equal(currentAnswer, SENTINEL_ANSWER, `dst answer_state.js must remain untouched on missing ${file.name}`);
        assert.equal(currentAdapter, SENTINEL_ADAPTER, `dst chat_adapter.bundle.js must remain untouched on missing ${file.name}`);
        assert.equal(currentMarkdown, SENTINEL_MARKDOWN, `dst markdown_render.js must remain untouched on missing ${file.name}`);
        assert.equal(currentProtocol, SENTINEL_PROTOCOL, `dst webview_protocol.js must remain untouched on missing ${file.name}`);
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

    // Verify webview_protocol.js execution in sandboxed CJS context
    const generatedProtocolBundle = await readFile(dstProtocolBundle, 'utf8');
    assert.notEqual(generatedProtocolBundle, SENTINEL_PROTOCOL);

    const protocolSandbox: any = {
      exports: {},
      module: { exports: {} },
      require: () => { throw new Error('Standalone bundle must not require external modules'); },
    };
    protocolSandbox.module.exports = protocolSandbox.exports;
    vm.createContext(protocolSandbox);
    vm.runInContext(generatedProtocolBundle, protocolSandbox);

    const exportedProtocol = protocolSandbox.module.exports;
    assert.equal(typeof exportedProtocol.parseWebviewToHostMessage, 'function');
    const parsedReady = exportedProtocol.parseWebviewToHostMessage({ kind: 'ready' });
    assert.ok(parsedReady);
    assert.equal(parsedReady.kind, 'ready');
    assert.equal(exportedProtocol.parseWebviewToHostMessage({ kind: 'say' }), undefined);

    // Verify markdown_render.js execution in sandboxed CJS context (§5.8.5)
    const generatedMarkdownRender = await readFile(dstMarkdownRender, 'utf8');
    assert.notEqual(generatedMarkdownRender, SENTINEL_MARKDOWN);

    const markdownSandbox: any = {
      exports: {},
      module: { exports: {} },
      require: () => { throw new Error('Standalone bundle must not require external modules'); },
    };
    markdownSandbox.module.exports = markdownSandbox.exports;
    vm.createContext(markdownSandbox);
    vm.runInContext(generatedMarkdownRender, markdownSandbox);

    const exportedMarkdown = markdownSandbox.module.exports;
    assert.equal(typeof exportedMarkdown.renderMarkdown, 'function');
    assert.equal(typeof exportedMarkdown.isSafeUrl, 'function');
    assert.equal(exportedMarkdown.isSafeUrl('https://example.com'), true);
    assert.equal(exportedMarkdown.isSafeUrl('command:workbench.action'), false);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test('§5.8.5 generate-third-party-licenses: validates presence, check mode, and required notices', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const licensesPath = path.join(rootDir, 'THIRD_PARTY_LICENSES.txt');
  assert.ok(existsSync(licensesPath), 'THIRD_PARTY_LICENSES.txt must exist');

  const content = await readFile(licensesPath, 'utf8');

  // Verify markdown-it copyright and MIT license notice
  assert.match(content, /Package:\s*markdown-it@/, 'markdown-it must be included in runtime third-party licenses');
  assert.match(content, /Copyright \(c\) 2014 Vitaly Puzrin, Alex Kocharin\./, 'markdown-it copyright notice must be present verbatim');
  assert.match(content, /Permission is hereby granted, free of charge/, 'markdown-it permission notice must be present verbatim');

  // Verify entities and valibot and rxjs
  assert.match(content, /Package:\s*entities@/);
  assert.match(content, /Package:\s*valibot@/);
  assert.match(content, /Package:\s*rxjs@/);

  // Verify tool --check mode succeeds via execFile
  const { execFile } = await import('node:child_process');
  const { promisify } = await import('node:util');
  const execFileAsync = promisify(execFile);
  const generatorScript = path.join(rootDir, 'tools', 'generate-third-party-licenses.mjs');
  const { stdout } = await execFileAsync(process.execPath, [generatorScript, '--check', `--root=${rootDir}`]);
  assert.match(stdout, /THIRD_PARTY_LICENSES\.txt is up to date/);
});

test('§5.8.5 VSIX package verification: rejects missing files via API and CLI with exit code 1 and path diagnostics', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const verifyScript = path.join(rootDir, 'tools', 'verify-vsix.mjs');
  const { verifyVsixArchive } = await import(path.join(rootDir, 'tools', 'verify-vsix.mjs') as any);
  const { execFile } = await import('node:child_process');
  const { promisify } = await import('node:util');
  const execFileAsync = promisify(execFile);

  // 1. API: Missing path throws error containing the non-existent path
  const nonExistentPath = path.join(rootDir, 'non-existent-artifact-999.vsix');
  await assert.rejects(
    async () => {
      await verifyVsixArchive(nonExistentPath, { rootDir });
    },
    (err: any) => {
      assert.ok(err.message.includes(nonExistentPath), 'Error message must contain missing VSIX path');
      return true;
    }
  );

  // 2. CLI: Invocation without arguments exits with code 1
  try {
    await execFileAsync(process.execPath, [verifyScript]);
    assert.fail('CLI without arguments should have failed with exit code 1');
  } catch (err: any) {
    assert.equal(err.code, 1, 'CLI without arguments must exit with code 1');
    assert.match(err.stderr, /Missing required VSIX archive path/);
  }

  // 3. CLI: Invocation with non-existent path exits with code 1 and reports path in stderr
  try {
    await execFileAsync(process.execPath, [verifyScript, nonExistentPath]);
    assert.fail('CLI with non-existent path should have failed with exit code 1');
  } catch (err: any) {
    assert.equal(err.code, 1, 'CLI with non-existent path must exit with code 1');
    assert.ok(err.stderr.includes(nonExistentPath), 'Stderr must include non-existent file path');
  }
});

test('§5.8.5 VSIX regression tests: rejects license mismatch, bundle mismatch, forbidden files, and validates valid archive with spaces/custom version', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const { verifyVsixArchive } = await import(path.join(rootDir, 'tools', 'verify-vsix.mjs') as any);
  const { execFile } = await import('node:child_process');
  const { promisify } = await import('node:util');
  const execFileAsync = promisify(execFile);

  const baseTempDir = await mkdtemp(path.join(tmpdir(), 'magi-vsix-reg-'));
  try {
    // Helper to create a zip/vsix archive from an extension folder layout
    const makeVsix = async (
      outVsixPath: string,
      customFiles: {
        version?: string;
        omitThirdParty?: boolean;
        tamperThirdParty?: boolean;
        tamperBundle?: boolean;
        includeNodeModules?: boolean;
        includeTestDir?: boolean;
      } = {}
    ) => {
      const stageDir = await mkdtemp(path.join(baseTempDir, 'stage-'));
      const extDir = path.join(stageDir, 'extension');
      await mkdir(path.join(extDir, 'out', 'web'), { recursive: true });

      // package.json
      const pkg = {
        name: 'magi',
        version: customFiles.version || '0.2.0',
        publisher: 'sayaya1090',
      };
      await writeFile(path.join(extDir, 'package.json'), JSON.stringify(pkg, null, 2));

      // LICENSE.txt
      const licenseBytes = await readFile(path.join(rootDir, 'LICENSE'));
      await writeFile(path.join(extDir, 'LICENSE.txt'), licenseBytes);

      // THIRD_PARTY_LICENSES.txt
      if (!customFiles.omitThirdParty) {
        let thirdPartyBytes = await readFile(path.join(rootDir, 'THIRD_PARTY_LICENSES.txt'));
        if (customFiles.tamperThirdParty) {
          thirdPartyBytes = Buffer.from(thirdPartyBytes.toString('utf8') + '\n// Tampered trailing line\n');
        }
        await writeFile(path.join(extDir, 'THIRD_PARTY_LICENSES.txt'), thirdPartyBytes);
      }

      // core-release.properties
      if (existsSync(path.join(rootDir, 'core-release.properties'))) {
        const coreProps = await readFile(path.join(rootDir, 'core-release.properties'));
        await writeFile(path.join(extDir, 'core-release.properties'), coreProps);
      }

      // Bundles
      let chatAdapterBytes = await readFile(path.join(rootDir, 'out', 'web', 'chat_adapter.bundle.js'));
      if (customFiles.tamperBundle) {
        chatAdapterBytes = Buffer.from('console.log("tampered bundle");');
      }
      await writeFile(path.join(extDir, 'out', 'web', 'chat_adapter.bundle.js'), chatAdapterBytes);

      const renderBytes = await readFile(path.join(rootDir, 'out', 'web', 'markdown_render.js'));
      await writeFile(path.join(extDir, 'out', 'web', 'markdown_render.js'), renderBytes);

      // Forbidden dirs if requested
      if (customFiles.includeNodeModules) {
        await mkdir(path.join(extDir, 'node_modules', 'evil-pkg'), { recursive: true });
        await writeFile(path.join(extDir, 'node_modules', 'evil-pkg', 'index.js'), '// evil');
      }
      if (customFiles.includeTestDir) {
        await mkdir(path.join(extDir, 'out', 'test'), { recursive: true });
        await writeFile(path.join(extDir, 'out', 'test', 'dummy.test.js'), '// test');
      }

      // Create zip archive
      await mkdir(path.dirname(outVsixPath), { recursive: true });
      await execFileAsync('zip', ['-rq', outVsixPath, 'extension'], { cwd: stageDir });
      await rm(stageDir, { recursive: true, force: true });
    };

    // Case 1: Missing THIRD_PARTY_LICENSES.txt
    const vsixMissingThirdParty = path.join(baseTempDir, 'missing-license.vsix');
    await makeVsix(vsixMissingThirdParty, { omitThirdParty: true });
    await assert.rejects(
      async () => {
        await verifyVsixArchive(vsixMissingThirdParty, { rootDir });
      },
      /THIRD_PARTY_LICENSES\.txt/
    );

    // Case 2: Tampered THIRD_PARTY_LICENSES.txt (byte-level mismatch)
    const vsixTamperedThirdParty = path.join(baseTempDir, 'tampered-license.vsix');
    await makeVsix(vsixTamperedThirdParty, { tamperThirdParty: true });
    await assert.rejects(
      async () => {
        await verifyVsixArchive(vsixTamperedThirdParty, { rootDir });
      },
      /byte-for-byte/
    );

    // Case 3: Version mismatch
    const vsixVersionMismatch = path.join(baseTempDir, 'version-mismatch.vsix');
    await makeVsix(vsixVersionMismatch, { version: '0.9.9' });
    await assert.rejects(
      async () => {
        await verifyVsixArchive(vsixVersionMismatch, { rootDir, expectedVersion: '1.0.0' });
      },
      /version mismatch/
    );

    // Case 4: Bundle tampering (stale or modified bundle)
    const vsixTamperedBundle = path.join(baseTempDir, 'tampered-bundle.vsix');
    await makeVsix(vsixTamperedBundle, { tamperBundle: true });
    await assert.rejects(
      async () => {
        await verifyVsixArchive(vsixTamperedBundle, { rootDir });
      },
      /chat_adapter\.bundle\.js must match built/
    );

    // Case 5: Forbidden node_modules directory included
    const vsixWithNodeModules = path.join(baseTempDir, 'with-node-modules.vsix');
    await makeVsix(vsixWithNodeModules, { includeNodeModules: true });
    await assert.rejects(
      async () => {
        await verifyVsixArchive(vsixWithNodeModules, { rootDir });
      },
      /node_modules/
    );

    // Case 6: Forbidden out/test directory included
    const vsixWithTest = path.join(baseTempDir, 'with-test.vsix');
    await makeVsix(vsixWithTest, { includeTestDir: true });
    await assert.rejects(
      async () => {
        await verifyVsixArchive(vsixWithTest, { rootDir });
      },
      /out\/test/
    );

    // Case 7: Valid custom archive with version 1.5.0 and path with spaces
    const spacePathDir = path.join(baseTempDir, 'magi custom space path dir');
    const validCustomVsix = path.join(spacePathDir, 'magi release 1.5.0.vsix');
    await makeVsix(validCustomVsix, { version: '1.5.0' });
    const verifyResult = await verifyVsixArchive(validCustomVsix, {
      rootDir,
      expectedVersion: '1.5.0',
    });
    assert.equal(verifyResult.version, '1.5.0');
    assert.ok(verifyResult.sizeBytes > 0);
    assert.ok(verifyResult.fileCount >= 5);
  } finally {
    await rm(baseTempDir, { recursive: true, force: true });
  }
});

test('§5.8.5: verifyVsixArchive and test suite remain hermetic across three root VSIX states (absent, stale, corrupted)', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const { verifyVsixArchive } = await import(path.join(rootDir, 'tools', 'verify-vsix.mjs') as any);
  const { execFile } = await import('node:child_process');
  const { promisify } = await import('node:util');
  const execFileAsync = promisify(execFile);

  const baseTempDir = await mkdtemp(path.join(tmpdir(), 'magi-vsix-hermetic-'));
  try {
    const mockRootDir = path.join(baseTempDir, 'mock-extension-root');
    const fixtureDir = path.join(baseTempDir, 'fixtures');
    await mkdir(mockRootDir, { recursive: true });
    await mkdir(fixtureDir, { recursive: true });

    // Copy core files to mockRootDir
    await writeFile(path.join(mockRootDir, 'package.json'), JSON.stringify({ name: 'magi', version: '0.2.0' }, null, 2));
    await cp(path.join(rootDir, 'LICENSE'), path.join(mockRootDir, 'LICENSE'));
    await cp(path.join(rootDir, 'THIRD_PARTY_LICENSES.txt'), path.join(mockRootDir, 'THIRD_PARTY_LICENSES.txt'));
    await mkdir(path.join(mockRootDir, 'out', 'web'), { recursive: true });
    await cp(path.join(rootDir, 'out', 'web', 'chat_adapter.bundle.js'), path.join(mockRootDir, 'out', 'web', 'chat_adapter.bundle.js'));
    await cp(path.join(rootDir, 'out', 'web', 'markdown_render.js'), path.join(mockRootDir, 'out', 'web', 'markdown_render.js'));

    // Create a valid VSIX fixture in fixtureDir
    const fixtureVsix = path.join(fixtureDir, 'hermetic-fixture.vsix');
    const stageDir = path.join(fixtureDir, 'stage');
    const extDir = path.join(stageDir, 'extension');
    await mkdir(path.join(extDir, 'out', 'web'), { recursive: true });
    await writeFile(path.join(extDir, 'package.json'), JSON.stringify({ name: 'magi', version: '0.2.0' }, null, 2));
    await cp(path.join(mockRootDir, 'LICENSE'), path.join(extDir, 'LICENSE.txt'));
    await cp(path.join(mockRootDir, 'THIRD_PARTY_LICENSES.txt'), path.join(extDir, 'THIRD_PARTY_LICENSES.txt'));
    await cp(path.join(mockRootDir, 'out', 'web', 'chat_adapter.bundle.js'), path.join(extDir, 'out', 'web', 'chat_adapter.bundle.js'));
    await cp(path.join(mockRootDir, 'out', 'web', 'markdown_render.js'), path.join(extDir, 'out', 'web', 'markdown_render.js'));
    await execFileAsync('zip', ['-q', '-r', fixtureVsix, 'extension'], { cwd: stageDir });

    const rootVsixPath = path.join(mockRootDir, 'magi-0.2.0.vsix');

    // State 1: No VSIX in root
    assert.equal(existsSync(rootVsixPath), false, 'State 1: Root VSIX must not exist');
    const res1 = await verifyVsixArchive(fixtureVsix, { rootDir: mockRootDir, expectedVersion: '0.2.0' });
    assert.equal(res1.version, '0.2.0');
    assert.ok(res1.sizeBytes > 0);

    // State 2: Stale/mismatched VSIX in root (tampered bundle)
    const staleStageDir = path.join(baseTempDir, 'stale-stage');
    const staleExtDir = path.join(staleStageDir, 'extension', 'out', 'web');
    await mkdir(staleExtDir, { recursive: true });
    await writeFile(path.join(staleStageDir, 'extension', 'package.json'), JSON.stringify({ name: 'magi', version: '0.1.0' }));
    await cp(path.join(mockRootDir, 'LICENSE'), path.join(staleStageDir, 'extension', 'LICENSE.txt'));
    await cp(path.join(mockRootDir, 'THIRD_PARTY_LICENSES.txt'), path.join(staleStageDir, 'extension', 'THIRD_PARTY_LICENSES.txt'));
    await writeFile(path.join(staleExtDir, 'chat_adapter.bundle.js'), '/* stale mismatched code */');
    await writeFile(path.join(staleExtDir, 'markdown_render.js'), '/* stale markdown */');
    await execFileAsync('zip', ['-q', '-r', rootVsixPath, 'extension'], { cwd: staleStageDir });

    const staleRootStatBefore = await stat(rootVsixPath);
    const res2 = await verifyVsixArchive(fixtureVsix, { rootDir: mockRootDir, expectedVersion: '0.2.0' });
    assert.equal(res2.version, '0.2.0');
    assert.equal(res2.sizeBytes, res1.sizeBytes);
    const staleRootStatAfter = await stat(rootVsixPath);
    assert.equal(staleRootStatAfter.mtimeMs, staleRootStatBefore.mtimeMs, 'State 2: Stale root VSIX must not be touched');

    // State 3: Corrupted file with same name in root
    await writeFile(rootVsixPath, 'corrupted non-zip content buffer');
    const res3 = await verifyVsixArchive(fixtureVsix, { rootDir: mockRootDir, expectedVersion: '0.2.0' });
    assert.equal(res3.version, '0.2.0');
    assert.equal(res3.sizeBytes, res1.sizeBytes);
    const corruptedContentAfter = await readFile(rootVsixPath, 'utf8');
    assert.equal(corruptedContentAfter, 'corrupted non-zip content buffer', 'State 3: Corrupted root file must remain untouched');
  } finally {
    await rm(baseTempDir, { recursive: true, force: true });
  }
});

test('§5.8.5: package-vsix resolvePackageConfig parses vsce options, target architectures, directories, and errors', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const { resolvePackageConfig } = await import(path.join(rootDir, 'tools', 'package-vsix.mjs') as any);
  const pkg = { name: 'magi', version: '0.2.0' };

  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-pkg-config-'));
  try {
    // 1. Default (no args)
    const cfgDefault = await resolvePackageConfig([], { rootDir, pkg });
    assert.equal(cfgDefault.targetVsix, path.resolve(rootDir, 'magi-0.2.0.vsix'));
    assert.equal(cfgDefault.targetArch, null);
    assert.equal(cfgDefault.outPath, null);
    assert.equal(cfgDefault.pkgVersion, '0.2.0');

    // 2. --target linux-x64
    const cfgTarget1 = await resolvePackageConfig(['--target', 'linux-x64'], { rootDir, pkg });
    assert.equal(cfgTarget1.targetVsix, path.resolve(rootDir, 'magi-linux-x64-0.2.0.vsix'));
    assert.equal(cfgTarget1.targetArch, 'linux-x64');

    // 3. -t win32-arm64
    const cfgTarget2 = await resolvePackageConfig(['-t', 'win32-arm64'], { rootDir, pkg });
    assert.equal(cfgTarget2.targetVsix, path.resolve(rootDir, 'magi-win32-arm64-0.2.0.vsix'));
    assert.equal(cfgTarget2.targetArch, 'win32-arm64');

    // 4. --target=darwin-arm64 and -t=alpine-x64
    const cfgTargetEq1 = await resolvePackageConfig(['--target=darwin-arm64'], { rootDir, pkg });
    assert.equal(cfgTargetEq1.targetVsix, path.resolve(rootDir, 'magi-darwin-arm64-0.2.0.vsix'));
    assert.equal(cfgTargetEq1.targetArch, 'darwin-arm64');

    const cfgTargetEq2 = await resolvePackageConfig(['-t=alpine-x64'], { rootDir, pkg });
    assert.equal(cfgTargetEq2.targetVsix, path.resolve(rootDir, 'magi-alpine-x64-0.2.0.vsix'));
    assert.equal(cfgTargetEq2.targetArch, 'alpine-x64');

    // 5. Explicit file with spaces via -o and --out=
    const customFilePath = path.join(tempDir, 'custom dir with space', 'my artifact 0.2.0.vsix');
    const cfgFile1 = await resolvePackageConfig(['-o', customFilePath], { rootDir, pkg });
    assert.equal(cfgFile1.targetVsix, customFilePath);

    const cfgFile2 = await resolvePackageConfig([`--out=${customFilePath}`], { rootDir, pkg });
    assert.equal(cfgFile2.targetVsix, customFilePath);

    // 6. Existing directory with spaces via --out
    const existingDirWithSpaces = path.join(tempDir, 'magi release folder with space');
    await mkdir(existingDirWithSpaces, { recursive: true });

    const cfgDir = await resolvePackageConfig(['--out', existingDirWithSpaces], { rootDir, pkg });
    assert.equal(cfgDir.targetVsix, path.join(existingDirWithSpaces, 'magi-0.2.0.vsix'));
    assert.ok(existsSync(existingDirWithSpaces), 'Existing directory must remain intact');

    // 7. Combined --target and --out directory
    const cfgTargetDir = await resolvePackageConfig(['--target', 'linux-x64', '--out', existingDirWithSpaces], { rootDir, pkg });
    assert.equal(cfgTargetDir.targetVsix, path.join(existingDirWithSpaces, 'magi-linux-x64-0.2.0.vsix'));

    // 8. Custom version and --allow-missing-repository
    const cfgVersion = await resolvePackageConfig(['0.3.5', '--allow-missing-repository'], { rootDir, pkg });
    assert.equal(cfgVersion.targetVsix, path.resolve(rootDir, 'magi-0.3.5.vsix'));
    assert.equal(cfgVersion.pkgVersion, '0.3.5');

    // 9. Combined custom version + target + out directory
    const cfgVerTargetDir = await resolvePackageConfig(['1.5.0', '--target', 'win32-x64', '--out', existingDirWithSpaces], { rootDir, pkg });
    assert.equal(cfgVerTargetDir.targetVsix, path.join(existingDirWithSpaces, 'magi-win32-x64-1.5.0.vsix'));
    assert.equal(cfgVerTargetDir.pkgVersion, '1.5.0');

    // 10. Duplicate options order handling (last one wins)
    const cfgDupOut = await resolvePackageConfig(['--out', 'first.vsix', '--out', 'second.vsix'], { rootDir, pkg });
    assert.equal(cfgDupOut.targetVsix, path.resolve(rootDir, 'second.vsix'));

    const cfgDupTarget = await resolvePackageConfig(['--target', 'linux-x64', '--target', 'win32-x64'], { rootDir, pkg });
    assert.equal(cfgDupTarget.targetArch, 'win32-x64');
    assert.equal(cfgDupTarget.targetVsix, path.resolve(rootDir, 'magi-win32-x64-0.2.0.vsix'));

    // 11. Missing option arguments validation
    await assert.rejects(async () => {
      await resolvePackageConfig(['--out'], { rootDir, pkg });
    }, /Missing argument for option: --out/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['-o'], { rootDir, pkg });
    }, /Missing argument for option: -o/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['--out='], { rootDir, pkg });
    }, /Missing argument for option: --out/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['-o='], { rootDir, pkg });
    }, /Missing argument for option: -o/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['--target'], { rootDir, pkg });
    }, /Missing argument for option: --target/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['-t'], { rootDir, pkg });
    }, /Missing argument for option: -t/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['--target='], { rootDir, pkg });
    }, /Missing argument for option: --target/);

    await assert.rejects(async () => {
      await resolvePackageConfig(['-t='], { rootDir, pkg });
    }, /Missing argument for option: -t/);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test('§5.8.5: packageVsix executes vsce with correct args, preserves directories, unlinks stale target files, and validates output', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const { packageVsix } = await import(path.join(rootDir, 'tools', 'package-vsix.mjs') as any);
  const pkg = { name: 'magi', version: '0.2.0' };

  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-pkg-wrapper-'));
  try {
    const outDir = path.join(tempDir, 'output directory with space');
    await mkdir(outDir, { recursive: true });

    // Pre-create stale artifact file in outDir
    const targetFile = path.join(outDir, 'magi-linux-x64-0.2.0.vsix');
    await writeFile(targetFile, 'stale content');

    let executedCommand = '';
    let executedArgs: string[] = [];
    let verifiedTarget = '';
    let verifiedVersion = '';

    const mockExecRunner = async (cmd: string, args: string[]) => {
      executedCommand = cmd;
      executedArgs = args;
      // When vsce executes, it generates targetFile
      await writeFile(targetFile, 'new valid package content');
      return { stdout: 'vsce mock success', stderr: '' };
    };

    const mockVerifyFn = async (vsixPath: string, opts: any) => {
      verifiedTarget = vsixPath;
      verifiedVersion = opts.expectedVersion;
      return { version: opts.expectedVersion, fileCount: 62, sizeBytes: 12345, vsixPath };
    };

    const res = await packageVsix(['--target', 'linux-x64', '--out', outDir, '--allow-missing-repository'], {
      rootDir,
      pkg,
      execRunner: mockExecRunner,
      verifyFn: mockVerifyFn,
    });

    assert.equal(res.version, '0.2.0');
    assert.equal(executedCommand, 'npx');
    assert.deepEqual(executedArgs, [
      '--yes',
      '@vscode/vsce',
      'package',
      '--no-dependencies',
      '--target',
      'linux-x64',
      '--out',
      outDir,
      '--allow-missing-repository',
    ]);
    assert.equal(verifiedTarget, targetFile);
    assert.equal(verifiedVersion, '0.2.0');
    assert.ok(existsSync(outDir), 'Output directory must be preserved');
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test('§5.8.5: packageVsix real vsce packaging for --target linux-x64 and --out directory', async () => {
  const rootDir = path.resolve(__dirname, '..', '..');
  const { packageVsix } = await import(path.join(rootDir, 'tools', 'package-vsix.mjs') as any);

  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-real-vsce-'));
  try {
    const outDir = path.join(tempDir, 'dist with space');
    await mkdir(outDir, { recursive: true });

    // Test real vsce packaging with --target linux-x64 and --out <outDir>
    const res = await packageVsix(['--target', 'linux-x64', '--out', outDir], { rootDir });
    assert.equal(res.version, '0.2.0');
    assert.ok(res.fileCount >= 60, `File count must be >= 60, got ${res.fileCount}`);
    assert.ok(res.sizeBytes > 0);
    assert.equal(res.vsixPath, path.join(outDir, 'magi-linux-x64-0.2.0.vsix'));
    assert.ok(existsSync(outDir), 'outDir must remain intact as a directory');
    assert.ok(existsSync(res.vsixPath), 'Output package must exist');
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});



