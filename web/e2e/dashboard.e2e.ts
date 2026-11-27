import { expect, test, type BrowserContext, type Page } from 'playwright/test'

const ADMIN_USERNAME = process.env.E2E_ADMIN_USERNAME ?? 'e2e-admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? 'E2e-Password-123!'

async function signIn(
  page: Page,
  username = ADMIN_USERNAME,
  password = ADMIN_PASSWORD
) {
  await page.goto('/sign-in')
  await page.getByTestId('sign-in-identifier').fill(username)
  await page.getByTestId('sign-in-password').fill(password)
  await page.getByTestId('sign-in-submit').click()
  await expect(page).not.toHaveURL(/\/sign-in/)
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('tianshu.lang', 'en-US')
  })
})

test('aggregates live database metrics on dashboard and enforces dashboard:read guard', async ({
  browser,
  page,
  request,
}) => {
  const suffix = Date.now()
  const restrictedUser = `e2e_nodash_${suffix}`
  const restrictedEmail = `${restrictedUser}@example.com`
  const restrictedPassword = 'NoDash-Password-123!'

  let adminToken = ''
  let createdUserId = ''
  let secondCtx: BrowserContext | null = null

  try {
    // 1. Admin signs in and lands on the live dashboard.
    await signIn(page)
    await page.goto('/')
    await expect(page.getByTestId('dashboard-page-title')).toBeVisible()
    await expect(page.getByTestId('dashboard-trend-chart')).toBeVisible()
    await expect(
      page.getByTestId('dashboard-recent-item').first()
    ).toBeVisible()

    const initialUsersText =
      (await page.getByTestId('dashboard-kpi-users-value').textContent()) ?? '0'
    const initialUsers = Number(initialUsersText.replaceAll(',', '').trim())
    expect(initialUsers).toBeGreaterThanOrEqual(1)

    // Switch time range to 14d and 30d and back to 7d.
    await page.getByTestId('dashboard-range-14').click()
    await expect(page.getByTestId('dashboard-range-14')).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    await page.getByTestId('dashboard-range-30').click()
    await expect(page.getByTestId('dashboard-range-30')).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    await page.getByTestId('dashboard-range-7').click()

    // 2. Create a user without dashboard:read via the real API, then verify live count increments.
    const loginRes = await request.post('/api/v1/auth/login', {
      data: { identifier: ADMIN_USERNAME, password: ADMIN_PASSWORD },
    })
    expect(loginRes.ok()).toBe(true)
    const loginBody = (await loginRes.json()) as { accessToken: string }
    adminToken = loginBody.accessToken

    const createRes = await request.post('/api/v1/users', {
      headers: { Authorization: `Bearer ${adminToken}` },
      data: {
        username: restrictedUser,
        email: restrictedEmail,
        displayName: 'Restricted E2E User',
        password: restrictedPassword,
        roleIds: [],
      },
    })
    expect(createRes.status()).toBe(201)
    const createdBody = (await createRes.json()) as { id: string }
    createdUserId = createdBody.id

    await page.getByTestId('dashboard-refresh-btn').click()
    await expect(page.getByTestId('dashboard-kpi-users-value')).toHaveText(
      String(initialUsers + 1)
    )

    // 3. Sign in as the user lacking dashboard:read in a separate context -> / redirects to /403.
    secondCtx = await browser.newContext()
    const secondPage = await secondCtx.newPage()
    await secondPage.addInitScript(() => {
      window.localStorage.setItem('tianshu.lang', 'en-US')
    })
    await signIn(secondPage, restrictedUser, restrictedPassword)
    await expect(secondPage).toHaveURL(/\/403/)
  } finally {
    if (secondCtx) {
      await secondCtx.close().catch(() => {})
    }
    if (adminToken && createdUserId) {
      await request
        .delete(`/api/v1/users/${createdUserId}`, {
          headers: { Authorization: `Bearer ${adminToken}` },
        })
        .catch(() => {})
    }
  }
})
