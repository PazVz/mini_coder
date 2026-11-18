import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  retries: 0,
  timeout: 30_000,
  forbidOnly: Boolean(process.env.CI),
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: process.env.MINICODER_BROWSER_URL,
    headless: true,
    launchOptions: { executablePath: process.env.CHROMIUM_PATH },
  },
})
