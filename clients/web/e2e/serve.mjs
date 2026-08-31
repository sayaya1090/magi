// 시험이 상대할 콘솔을 세운다 — 진짜 데몬 하나와, 그것을 서빙하는 진짜 magi-web.
//
// 정적 데모로도 화면은 뜨지만, 데모에서는 명단이 흐르지 않는다. 이 시험들이 재는 것의 절반은
// "열어 두기만 해도 무엇이 몇 번 나가는가"이고, 그 조건은 명단이 실제로 흐를 때만 생긴다 —
// 데모를 상대로 재면 규칙을 걷어내도 초록이 나온다(실측했다).
//
// 데몬은 턴을 돌리지 않는다. 이 화면들이 묻는 것(명단·잡·크론·모델 목록·승인 모드)은 전부
// 그 프로세스가 제 상태에서 답하는 것이라, 모델도 열쇠도 없이 선다.
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, existsSync, openSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repo = resolve(process.cwd(), '../../..');
const port = Number(process.env.MAGI_E2E_PORT || 8199);
const bin = join(tmpdir(), 'magi-e2e-bin');
const web = join(tmpdir(), 'magi-e2e-web');

function build() {
  for (const [out, pkg] of [[bin, './cmd/magi'], [web, './clients/web/server']]) {
    const r = spawnSync('go', ['build', '-o', out, pkg], { cwd: repo, stdio: 'inherit' });
    if (r.status !== 0) throw new Error(`build ${pkg} failed`);
  }
}

const console_ = join(repo, 'clients/web/ui/build/console');
if (!existsSync(join(console_, 'console.html'))) {
  throw new Error(`no assembled console at ${console_} — run: cd clients/web/ui && ./gradlew assembleConsole`);
}
build();

// 짧은 뿌리에서 만든다. 유닉스 소켓 경로에는 한계가 있고(macOS ~104자) 이 기계의 기본 임시
// 디렉토리는 그 대부분을 이름에 쓴다 — 길면 데몬이 바인드에 실패하고, 화면은 "여기 도는 magi가
// 없다"를 정직하게 그린다. 이 저장소의 Go 시험이 /tmp를 쓰는 이유와 같다.
const shortRoot = existsSync('/tmp') ? '/tmp' : tmpdir();
const cfg = mkdtempSync(join(shortRoot, 'mge-c'));
const env = { ...process.env, MAGI_CONFIG_DIR: cfg };

// 둘을 세운다. 하나뿐인 명단에서는 「컴패니언마다 한 번」과 「콘솔마다 한 번」이 같은 수를
// 내므로, 그 둘을 가르는 시험은 이웃이 있어야만 답을 낸다 — 하나면 그 시험은 조용히 건너뛴다.
const spaces = [mkdtempSync(join(shortRoot, 'mge-w')), mkdtempSync(join(shortRoot, 'mge-w'))];

// 로그는 버리지 않는다: 조용히 죽은 데몬은 "컴패니언이 없다"로 보이고, 그것은 시험이 잴 수
// 있는 실패가 아니라 시험이 못 보는 실패다.
const log = openSync(join(cfg, 'harness.log'), 'a');
const daemons = spaces.map((ws) => {
  const d = spawn(bin, ['--daemon'], { cwd: ws, env, stdio: ['ignore', log, log] });
  d.on('exit', (code) => console.error(`magi --daemon in ${ws} exited: ${code} (see ${join(cfg, 'harness.log')})`));
  return d;
});
const server = spawn(web, ['-addr', `127.0.0.1:${port}`, '-console', console_], { env, stdio: ['ignore', log, log] });
console.log(`e2e: config=${cfg} workspaces=${spaces.join(' ')} port=${port}`);

// 시험은 다른 프로세스다. 이 하네스가 어디에 세웠는지를 알아야 화면이 말하는 경로가 <b>진짜
// 그 경로인지</b> 대조할 수 있다 — 모르면 "무언가 그렸다"까지밖에 못 잰다.
writeFileSync(join(process.cwd(), '.harness.json'),
  JSON.stringify({ config: cfg, workspaces: spaces, log: join(cfg, 'harness.log') }, null, 2));

const stop = () => { for (const d of daemons) { try { d.kill(); } catch {} } try { server.kill(); } catch {} };
process.on('exit', stop);
process.on('SIGINT', () => { stop(); process.exit(130); });
process.on('SIGTERM', () => { stop(); process.exit(143); });

// 이 프로세스는 서버가 사는 동안 산다 — playwright의 webServer가 이것을 잡고 있는다.
await new Promise(() => {});
