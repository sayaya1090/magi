// 목업을 띄우는 정적 서버. 의존성 없다(node 만 있으면 된다).
//
// 두 갈래로 뜬다. 이건 편의가 아니라 **오늘 검증할 수 있는 것과 없는 것의 경계**다.
//
//   HTTPS — 인증서가 이미 있으면. Office 가 애드인 소스를 https 로만 받는다.
//   HTTP  — 없으면. Office 는 여기 못 붙지만 **맨 브라우저 목업은 그대로 돈다**(FakeDeck).
//
// 인증서가 없을 때 이 스크립트가 **직접 만들지 않는다.** `office-addin-dev-certs` 는 개발용 CA 를
// 시스템 키체인에 심는데, 그건 이 스크립트가 사용자 대신 내릴 결정이 아니다. 명령만 적어 준다.
import { createServer as createHttp } from 'node:http';
import { createServer as createHttps } from 'node:https';
import { readFile, stat } from 'node:fs/promises';
import { existsSync, readFileSync } from 'node:fs';
import { join, extname, resolve, normalize } from 'node:path';
import { homedir } from 'node:os';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(fileURLToPath(new URL('../', import.meta.url)));
const PORT = Number(process.env.PORT || 3000);

const CERT_DIR = join(homedir(), '.office-addin-dev-certs');
const CERT = join(CERT_DIR, 'localhost.crt');
const KEY = join(CERT_DIR, 'localhost.key');

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.xml': 'text/xml; charset=utf-8',
};

async function handler(req, res) {
  const url = new URL(req.url, 'https://localhost');
  let rel = decodeURIComponent(url.pathname);
  if (rel === '/') rel = '/taskpane.html';
  // 루트 밖으로 나가는 경로는 안 연다. 목업이라도 서버는 서버다.
  const path = join(ROOT, normalize(rel).replace(/^(\.\.[/\\])+/, ''));
  if (!path.startsWith(ROOT)) {
    res.writeHead(403).end('403');
    return;
  }
  try {
    const s = await stat(path);
    if (s.isDirectory()) throw new Error('dir');
    const body = await readFile(path);
    res.writeHead(200, {
      'Content-Type': TYPES[extname(path)] || 'application/octet-stream',
      'Cache-Control': 'no-store', // 목업은 고치면서 본다. 캐시가 거짓말하면 안 된다.
    });
    res.end(body);
  } catch {
    res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
    res.end('404 ' + rel);
  }
}

const haveCerts = existsSync(CERT) && existsSync(KEY);
const server = haveCerts
  ? createHttps({ cert: readFileSync(CERT), key: readFileSync(KEY) }, handler)
  : createHttp(handler);
const scheme = haveCerts ? 'https' : 'http';

server.listen(PORT, () => {
  console.log(`  ${scheme}://localhost:${PORT}/taskpane.html`);
  if (haveCerts) {
    console.log('  인증서를 찾았다. PowerPoint 에 사이드로드해도 된다(README 참고).');
  } else {
    console.log('');
    console.log('  ⚠ HTTP 로 떴다. **브라우저 목업은 이대로 돈다**(FakeDeck 으로 붙는다).');
    console.log('    PowerPoint 에 붙이려면 https 가 필요하고, 인증서는 직접 만들어야 한다:');
    console.log('');
    console.log('      npx office-addin-dev-certs install');
    console.log('');
    console.log('    개발용 CA 를 키체인에 심는 명령이라 이 스크립트가 대신 하지 않는다.');
  }
});
