import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import esbuild from 'esbuild';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

let root = path.join(__dirname, '..');
let isCheckOnly = false;

for (const arg of process.argv.slice(2)) {
  if (arg.startsWith('--root=')) {
    root = path.resolve(arg.slice(7));
  } else if (arg === '--check') {
    isCheckOnly = true;
  } else if (!arg.startsWith('--')) {
    root = path.resolve(arg);
  }
}

const targetPath = path.join(root, 'THIRD_PARTY_LICENSES.txt');
const adapterEntry = path.join(root, 'src', 'web', 'chat_adapter.ts');
const markdownRenderEntry = path.join(root, 'src', 'web', 'markdown_render.ts');
const nodeModulesDir = path.join(root, 'node_modules');

// 1. Discover all packages bundled into runtime webview assets via esbuild metafile
const res1 = esbuild.buildSync({
  entryPoints: [adapterEntry],
  bundle: true,
  format: 'iife',
  write: false,
  metafile: true,
  nodePaths: [nodeModulesDir],
});

const res2 = esbuild.buildSync({
  entryPoints: [markdownRenderEntry],
  bundle: true,
  platform: 'node',
  write: false,
  metafile: true,
  nodePaths: [nodeModulesDir],
});

const allInputs = { ...res1.metafile.inputs, ...res2.metafile.inputs };
const packageNames = new Set();
for (const inputPath of Object.keys(allInputs)) {
  const match = inputPath.match(/node_modules\/((?:@[^/]+\/)?[^/]+)/);
  if (match) {
    packageNames.add(match[1]);
  }
}

const sortedPackages = Array.from(packageNames).sort();

// 2. Collect package metadata and license texts
const sections = [];
sections.push(
`MAGI VS CODE EXTENSION - THIRD-PARTY SOFTWARE LICENSES
================================================================================
This file contains licensing and copyright notices for third-party software
dependencies bundled into the magi VS Code extension runtime webview assets.
Generated automatically by tools/generate-third-party-licenses.mjs.
Do not edit directly.
================================================================================
`
);

for (const pkg of sortedPackages) {
  const pkgDir = path.join(nodeModulesDir, pkg);
  if (!fs.existsSync(pkgDir)) {
    console.error(`Package directory not found: ${pkgDir}`);
    process.exit(1);
  }

  const pkgJsonPath = path.join(pkgDir, 'package.json');
  const pkgJson = JSON.parse(fs.readFileSync(pkgJsonPath, 'utf8'));
  const version = pkgJson.version || 'unknown';
  const licenseType = pkgJson.license || (pkgJson.licenses ? JSON.stringify(pkgJson.licenses) : 'unknown');

  const files = fs.readdirSync(pkgDir);
  const licFile = files.find(f => /^(licen[sc]e|copying|notice)(\b|[-_.])/i.test(f));

  if (!licFile) {
    console.error(`No license file found for runtime package: ${pkg}`);
    process.exit(1);
  }

  // 줄 끝은 **LF 로 통일한다** — 그러지 않으면 이 파일은 의존성이 무엇을 담아 왔느냐에 따라 달라지고,
  // `--check` 는 바이트를 그대로 견주므로 그 차이가 곧 「낡았다」가 된다.
  //
  // ⚠ 실측 2026-09-19: `node_modules/tslib/LICENSE.txt` 은 **npm 타르볼 자체가 CRLF** 다(11줄).
  // 같은 폴더의 rxjs·markdown-it 은 LF 다. 그래서 생성본은 CRLF 11줄을 담고, 커밋된 파일은 0줄이라
  // `npm run package` 가 제 검사 문턱에서 멈춘다 — 「THIRD_PARTY_LICENSES.txt is outdated」. 아무도
  // 아무것도 안 바꿨는데. 이 기계의 `core.autocrlf` 는 false 이고 이 경로에 `.gitattributes` 규칙도
  // 없으니 git 이 손댄 것이 아니라, **의존성이 담아 온 바이트가 그대로 새어 나온 것**이다.
  //
  // 고지 내용은 바뀌지 않는다. 바뀌는 것은 같은 입력이 같은 출력을 낸다는 것뿐이고, 그것이 없으면
  // 이 검사는 무엇을 지키는 검사가 아니라 어느 기계에서 만들었는지를 재는 검사다.
  const licContent = fs.readFileSync(path.join(pkgDir, licFile), 'utf8').replace(/\r\n/g, '\n').trim();

  sections.push(
`--------------------------------------------------------------------------------
Package: ${pkg}@${version}
License: ${licenseType}
File:    ${licFile}
--------------------------------------------------------------------------------
${licContent}
`
  );
}

const generatedText = sections.join('\n') + '\n';

// 3. Verify required notices are present (§5.8.5)
const requiredNotices = [
  { name: 'markdown-it', pattern: /Copyright \(c\) 2014 Vitaly Puzrin, Alex Kocharin\./ },
  { name: 'markdown-it MIT permission', pattern: /Permission is hereby granted, free of charge/ },
  { name: 'entities', pattern: /Copyright \(c\) Felix Böhm/ },
  { name: 'valibot', pattern: /Copyright \(c\) Fabian Hiller/ },
  { name: 'rxjs', pattern: /Apache License/ },
];

for (const req of requiredNotices) {
  if (!req.pattern.test(generatedText)) {
    console.error(`Missing required license notice for ${req.name}: pattern ${req.pattern} not matched`);
    process.exit(1);
  }
}

if (isCheckOnly) {
  if (!fs.existsSync(targetPath)) {
    console.error(`THIRD_PARTY_LICENSES.txt does not exist at ${targetPath}. Run 'node tools/generate-third-party-licenses.mjs' to generate.`);
    process.exit(1);
  }
  const existing = fs.readFileSync(targetPath, 'utf8');
  if (existing !== generatedText) {
    console.error(`THIRD_PARTY_LICENSES.txt is outdated. Run 'node tools/generate-third-party-licenses.mjs' to update.`);
    process.exit(1);
  }
  console.log(`✓ THIRD_PARTY_LICENSES.txt is up to date (${sortedPackages.length} packages).`);
} else {
  fs.writeFileSync(targetPath, generatedText, 'utf8');
  console.log(`Generated ${targetPath} with ${sortedPackages.length} packages: ${sortedPackages.join(', ')}`);
}
