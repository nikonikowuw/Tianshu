import { defineConfig, devices } from 'playwright/test'

const webPort = Number(process.env.E2E_WEB_PORT ?? 15173)
const apiPort = Number(process.env.E2E_API_PORT ?? 18080)
const baseURL = `http://127.0.0.1:${webPort}`
const apiURL = `http://127.0.0.1:${apiPort}`

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI
    ? [['list'], ['html', { open: 'never' }]]
    : [['list']],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    acceptDownloads: true,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: [
    {
      command: `pnpm dev --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: baseURL,
      reuseExistingServer: false,
      timeout: 120_000,
      env: {
        VITE_PROXY_TARGET: apiURL,
        VITE_DISABLE_DEVTOOLS: 'true',
      },
    },
    {
      command: 'bash ./e2e/start-backend.sh',
      url: `${apiURL}/healthz`,
      reuseExistingServer: false,
      timeout: 180_000,
      env: {
        E2E_API_PORT: String(apiPort),
      },
    },
  ],
})
