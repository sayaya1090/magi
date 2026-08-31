import { defineConfig, devices } from '@playwright/test';

// 시험이 상대하는 것은 진짜 magi-web과 진짜 데몬이다(serve.mjs). 정적 데모로도 화면은 뜨지만
// 거기서는 명단이 흐르지 않아, "열어 두기만 해도 몇 번 묻는가"를 재면 규칙을 걷어내도 초록이
// 나온다 — 실측했고, 그래서 이 시험들은 도는 것을 상대한다.
const port = Number(process.env.MAGI_E2E_PORT || 8199);

export default defineConfig({
  testDir: './tests',
  // 화면 하나가 느리다고 스위트가 멈추지 않게. 이 시험들은 시계를 기다리는 것이 있어(옆 판의
  // 5초 주기) 기본값보다 넉넉하다.
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [['github'], ['list']] : [['list']],
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: process.env.CI ? 'retain-on-failure' : 'off',
    ...devices['Desktop Chrome'],
  },
  webServer: {
    command: 'node ./serve.mjs',
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: !process.env.CI,
    // 첫 회는 go build 둘이 들어간다.
    timeout: 180_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
