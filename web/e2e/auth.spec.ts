import { expect, test, type Page } from '@playwright/test'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'

if (!process.env.MINICODER_BROWSER_URL || !process.env.MINICODER_BROWSER_SETUP_TOKEN) {
  throw new Error('请从仓库根目录运行 make test-browser，由 Go 创建隔离的测试服务。')
}

const password = 'browser-test-password-123'

async function expectProofNotPersisted(page: Page, link: string) {
  const savedState = await page.evaluate(() =>
    JSON.stringify({
      local: Object.entries(localStorage),
      session: Object.entries(sessionStorage),
      history: history.state,
    }),
  )
  // Vue Router may persist scroll positions; email proofs must never be persisted.
  expect(savedState).not.toContain(new URLSearchParams(new URL(link).hash.slice(1)).get('token'))
}

test('initialize, register, restore sessions, confirm email after login, and log out', async ({
  page,
  context,
  request,
}) => {
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))
  await page.goto('/')
  await expect(page).toHaveURL(/\/auth\/setup$/)
  await page.getByLabel('初始化令牌').fill(process.env.MINICODER_BROWSER_SETUP_TOKEN!)
  await page.getByLabel('用户名', { exact: true }).fill('admin')
  await page.getByLabel('邮箱', { exact: true }).fill('admin@example.com')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '创建管理员' }).click()
  await expect(page.getByRole('status')).toContainText('管理员已创建')
  await page.getByRole('link', { name: '前往登录', exact: true }).click()
  await page.getByLabel('用户名', { exact: true }).fill('admin')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/account$/)
  if (process.env.MINICODER_BROWSER_SCREENSHOT)
    await page.screenshot({
      path: process.env.MINICODER_BROWSER_SCREENSHOT + '-desktop.png',
      fullPage: true,
    })
  await page.getByRole('button', { name: '验证当前邮箱' }).click()
  await expect(page.getByRole('status').first()).toContainText('验证邮件已提交')
  const adminMail = await (await request.get('/__test/mail?email=admin@example.com')).json()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/auth\/login$/)
  await page.goto(adminMail.link)
  await expect(page).toHaveURL(/\/auth\/email\/confirm$/)
  await expectProofNotPersisted(page, adminMail.link)
  await expect(page.getByText('请在此登录，登录后继续确认邮箱。')).toBeVisible()
  await page.getByLabel('用户名', { exact: true }).fill('admin')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  // Logging in does not consume the proof; the explicit button still must be clicked.
  await expect(page.getByRole('button', { name: '确认邮箱', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '确认邮箱', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('邮箱已确认')
  await page.getByRole('link', { name: '查看我的账号' }).click()
  await expect(page.getByText('已验证', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/auth\/login$/)

  await page.getByRole('link', { name: '使用邮箱注册' }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByLabel('邮箱', { exact: true }).fill('member@example.com')
  if (process.env.MINICODER_BROWSER_SCREENSHOT)
    await page.screenshot({ path: process.env.MINICODER_BROWSER_SCREENSHOT, fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: '发送验证邮件' }).click()
  await expect(page.getByRole('status')).toContainText('验证邮件已提交')
  await expect(page.getByRole('button', { name: /秒后可重新发送/ })).toBeDisabled()
  const mail = await (await request.get('/__test/mail?email=member@example.com')).json()
  expect(new URL(mail.link).hash).toContain('token=')
  await page.goto(mail.link)
  await expect(page).toHaveURL(/\/auth\/register$/)
  await expectProofNotPersisted(page, mail.link)
  const username = page.getByLabel('用户名', { exact: true })
  await username.fill('Invalid Name')
  expect(await username.evaluate((input: HTMLInputElement) => input.validity.patternMismatch)).toBe(
    true,
  )
  await username.fill('new-member')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '完成注册', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('注册成功')
  expect((await context.cookies()).some((cookie) => cookie.name === 'minicoder_session')).toBe(
    false,
  )
  await page.getByRole('link', { name: '前往登录', exact: true }).first().click()
  await page.getByLabel('用户名', { exact: true }).fill('new-member')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/account$/)
  await expect(page.getByText('member@example.com', { exact: false })).toBeVisible()
  await expect(page.getByText('已验证', { exact: true })).toBeVisible()
  const cookie = (await context.cookies()).find((value) => value.name === 'minicoder_session')
  expect(cookie).toMatchObject({ httpOnly: true, sameSite: 'Lax', path: '/' })
  await page.reload()
  await expect(page.getByText('new-member', { exact: true })).toBeVisible()
  await expectProofNotPersisted(page, mail.link)
  await page.getByText('更换邮箱', { exact: true }).click()
  await page.getByLabel('新邮箱', { exact: true }).fill('updated@example.com')
  await page.getByLabel('当前密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '发送换址验证邮件' }).click()
  await expect(page.getByRole('status').first()).toContainText('确认前，当前邮箱保持不变')
  await expect(page.getByText('member@example.com', { exact: false })).toBeVisible()
  const changedMail = await (await request.get('/__test/mail?email=updated@example.com')).json()
  await page.goto(changedMail.link)
  await expect(page).toHaveURL(/\/auth\/email\/confirm$/)
  await page.getByRole('button', { name: '确认邮箱', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('邮箱已确认')
  await page.getByRole('link', { name: '查看我的账号' }).click()
  await expect(page.getByText('updated@example.com', { exact: false })).toBeVisible()
  await expect(page.getByText('已验证', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/auth\/login$/)
  await page.goto('/account')
  await expect(page).toHaveURL(/\/auth\/login$/)

  const csrf = await request.post('/api/v1/auth/login', {
    headers: { Origin: 'https://foreign.example' },
    data: { username: 'admin', password },
  })
  expect(csrf.status()).toBe(403)
  expect((await csrf.json()).code).toBe('csrf_failed')
  const missing = await request.get('/api/v1/unknown')
  expect(missing.status()).toBe(404)
  expect(missing.headers()['content-type']).toContain('application/json')
  expect(pageErrors).toEqual([])
})

test('Vite proxies API requests without relaxing the origin check', async ({ page, request }) => {
  const server = await createServer({
    configFile: fileURLToPath(new URL('../vite.config.ts', import.meta.url)),
    server: {
      port: 0,
      proxy: { '/api': { target: process.env.MINICODER_BROWSER_URL } },
    },
  })
  await server.listen()
  try {
    const address = server.httpServer!.address()
    if (!address || typeof address === 'string') throw new Error('Vite did not open a TCP listener')
    const origin = 'http://127.0.0.1:' + address.port
    await page.goto(origin + '/auth/login')
    await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
    await expect(page.getByLabel('用户名', { exact: true })).toBeVisible()
    const sameOrigin = await request.post(origin + '/api/v1/auth/logout', {
      headers: { Origin: origin },
      data: {},
    })
    expect(sameOrigin.status()).toBe(204)
    const foreignOrigin = await request.post(origin + '/api/v1/auth/logout', {
      headers: { Origin: 'https://foreign.example' },
      data: {},
    })
    expect(foreignOrigin.status()).toBe(403)
    expect((await foreignOrigin.json()).code).toBe('csrf_failed')
  } finally {
    await server.close()
  }
})
