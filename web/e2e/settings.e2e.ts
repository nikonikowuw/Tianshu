import { expect, test, type BrowserContext, type Page } from 'playwright/test'

const ADMIN_USERNAME = process.env.E2E_ADMIN_USERNAME ?? 'e2e-admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? 'E2e-Password-123!'

// 1x1 solid PNG valid for avatar re-encoding and inline rendering.
const TINY_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
  'base64'
)

async function signIn(page: Page, password = ADMIN_PASSWORD) {
  await page.goto('/sign-in')
  await page.getByTestId('sign-in-identifier').fill(ADMIN_USERNAME)
  await page.getByTestId('sign-in-password').fill(password)
  await page.getByTestId('sign-in-submit').click()
  await expect(page).not.toHaveURL(/\/sign-in/)
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('tianshu.lang', 'en-US')
  })
})

test('manages profile, avatar, appearance, active sessions, and password end-to-end', async ({
  browser,
  page,
}) => {
  await signIn(page)
  await page.goto('/settings')
  await expect(page.getByTestId('settings-page-title')).toBeVisible()

  // Verify switching across all 4 settings tabs causes zero horizontal or inset-frame jitter.
  const measureGeometry = async () => {
    const insetBox = await page
      .locator('[data-slot="sidebar-inset"]')
      .boundingBox()
    const titleBox = await page.getByTestId('settings-page-title').boundingBox()
    return { insetBox, titleBox }
  }
  const baseGeo = await measureGeometry()
  expect(baseGeo.insetBox).not.toBeNull()
  expect(baseGeo.titleBox).not.toBeNull()
  for (const route of [
    '/settings/security',
    '/settings/sessions',
    '/settings/appearance',
    '/settings',
  ]) {
    await page.goto(route)
    await expect(page.getByTestId('settings-page-title')).toBeVisible()
    const geo = await measureGeometry()
    expect(geo.insetBox).not.toBeNull()
    expect(geo.titleBox).not.toBeNull()
    expect(geo.insetBox?.width).toBe(baseGeo.insetBox?.width)
    expect(geo.insetBox?.height).toBe(baseGeo.insetBox?.height)
    expect(geo.titleBox?.x).toBe(baseGeo.titleBox?.x)
    expect(geo.titleBox?.width).toBe(baseGeo.titleBox?.width)
  }

  // 1. Update display name and verify sidebar + reload persistence.
  const displayInput = page.getByTestId('profile-display-name')
  await expect(displayInput).toBeVisible()
  const originalName = await displayInput.inputValue()
  const nextName = `E2E Admin ${Date.now()}`

  let secondCtx: BrowserContext | null = null
  let passwordChanged = false
  let displayNameChanged = false

  try {
    await displayInput.fill(nextName)
    await page.getByTestId('profile-submit').click()
    await expect(page.getByTestId('nav-user-trigger')).toContainText(nextName)
    displayNameChanged = true

    await page.reload()
    await expect(page.getByTestId('profile-display-name')).toHaveValue(nextName)

    // 2. Upload avatar, verify rendered image naturalWidth > 0, then remove it.
    await page.getByTestId('profile-avatar-input').setInputFiles({
      name: 'e2e-avatar.png',
      mimeType: 'image/png',
      buffer: TINY_PNG,
    })
    const avatarImg = page.getByTestId('profile-avatar-image')
    await expect(avatarImg).toBeVisible()
    await expect
      .poll(() =>
        avatarImg.evaluate((img: HTMLImageElement) => img.naturalWidth)
      )
      .toBeGreaterThan(0)

    await page.getByTestId('profile-avatar-remove').click()
    await expect(page.getByTestId('profile-avatar-remove')).toHaveCount(0)

    // 3. Switch theme and language on Appearance page and verify DOM/i18n reaction.
    await page.goto('/settings/appearance')
    await page.getByTestId('appearance-theme-dark').click()
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.classList.contains('dark'))
      )
      .toBe(true)
    await page.getByTestId('appearance-lang-zh-CN').click()
    await expect(page.getByTestId('settings-page-title')).toHaveText('个人设置')
    await page.getByTestId('appearance-lang-en-US').click()
    await page.getByTestId('appearance-theme-light').click()

    // 4. Active sessions: clean up leftover sessions from earlier tests via bulk revoke,
    //    then sign in from a second isolated context and revoke it individually.
    await page.goto('/settings/sessions')
    const cards = page.getByTestId('session-card')
    await expect(page.getByTestId('session-current-badge')).toBeVisible()
    const revokeOthersBtn = page.getByTestId('sessions-revoke-all-btn')
    if (await revokeOthersBtn.isVisible()) {
      await revokeOthersBtn.click()
      await page.getByTestId('sessions-revoke-all-confirm').click()
    }
    await expect(cards).toHaveCount(1)

    secondCtx = await browser.newContext()
    const secondPage = await secondCtx.newPage()
    await secondPage.addInitScript(() => {
      window.localStorage.setItem('tianshu.lang', 'en-US')
    })
    await signIn(secondPage)
    await secondPage.goto('/settings/sessions')
    await expect(secondPage.getByTestId('session-current-badge')).toBeVisible()

    await page.reload()
    await expect(cards).toHaveCount(2)
    await expect(page.getByTestId('session-current-badge')).toBeVisible()

    // Revoke the second device session.
    await page.getByTestId('session-revoke-btn').first().click()
    await page.getByTestId('session-revoke-confirm').click()
    await expect(cards).toHaveCount(1)

    // Live interaction on the kicked second context (without reload) gets 401 ->
    // refresh fails -> immediately redirects to /sign-in?redirect=...
    await secondPage.getByTestId('sessions-refresh-btn').click()
    await expect(secondPage).toHaveURL(/\/sign-in\?redirect=/)

    // 5. Security: wrong current password shows inline field error without logging out.
    await page.goto('/settings/security')
    await page.getByTestId('security-current-password').fill('WrongPass-999!')
    await page.getByTestId('security-new-password').fill('E2e-NewPass-456!')
    await page.getByTestId('security-confirm-password').fill('E2e-NewPass-456!')
    await page.getByTestId('security-submit').click()
    await expect(page).toHaveURL(/\/settings\/security/)
    await expect(page.getByTestId('security-current-password')).toHaveAttribute(
      'aria-invalid',
      'true'
    )

    // Change to new password and verify second device can sign in with it.
    await page.getByTestId('security-current-password').fill(ADMIN_PASSWORD)
    await page.getByTestId('security-submit').click()
    await expect(page.getByTestId('security-current-password')).toHaveValue('')
    passwordChanged = true

    await signIn(secondPage, 'E2e-NewPass-456!')
  } finally {
    // Restore original password and display name cleanly with bounded waits.
    if (passwordChanged) {
      await page.goto('/settings/security').catch(() => {})
      const currentPwdInput = page.getByTestId('security-current-password')
      const canRestorePwd = await currentPwdInput
        .waitFor({ state: 'visible', timeout: 10_000 })
        .then(
          () => true,
          () => false
        )
      if (canRestorePwd) {
        await currentPwdInput.fill('E2e-NewPass-456!')
        await page.getByTestId('security-new-password').fill(ADMIN_PASSWORD)
        await page.getByTestId('security-confirm-password').fill(ADMIN_PASSWORD)
        await page.getByTestId('security-submit').click()
        await expect(currentPwdInput)
          .toHaveValue('')
          .catch(() => {})
      }
    }
    if (displayNameChanged) {
      await page.goto('/settings').catch(() => {})
      const profileNameInput = page.getByTestId('profile-display-name')
      const canRestoreName = await profileNameInput
        .waitFor({ state: 'visible', timeout: 10_000 })
        .then(
          () => true,
          () => false
        )
      if (canRestoreName) {
        await profileNameInput.fill(originalName || 'E2E Admin')
        await page.getByTestId('profile-submit').click()
        await expect(page.getByTestId('profile-submit'))
          .toBeDisabled()
          .catch(() => {})
      }
    }
    if (secondCtx) {
      await secondCtx.close().catch(() => {})
    }
  }
})
