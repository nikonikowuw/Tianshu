import { expect, test, type Page } from 'playwright/test'

const ADMIN_USERNAME = process.env.E2E_ADMIN_USERNAME ?? 'e2e-admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? 'E2e-Password-123!'

// 1x1 transparent PNG, valid for both the upload allowlist and inline preview.
const TINY_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
  'base64'
)

async function signIn(page: Page) {
  await page.goto('/sign-in')
  await page.getByTestId('sign-in-identifier').fill(ADMIN_USERNAME)
  await page.getByTestId('sign-in-password').fill(ADMIN_PASSWORD)
  await page.getByTestId('sign-in-submit').click()
  await expect(page).not.toHaveURL(/\/sign-in/)
}

async function openFilesPage(page: Page) {
  await page.goto('/files')
  await expect(page.getByTestId('files-page-title')).toBeVisible()
}

async function uploadFile(
  page: Page,
  file: { name: string; mimeType: string; buffer: Buffer }
) {
  await page.getByTestId('files-upload-open').click()
  await page.getByTestId('files-upload-input').setInputFiles({
    name: file.name,
    mimeType: file.mimeType,
    buffer: file.buffer,
  })
  await page.getByTestId('files-upload-start').click()
  await expect(page.getByTestId('files-upload-success')).toBeVisible()
  await page.getByTestId('files-upload-close').click()
}

async function deleteFileRow(page: Page, fileName: string) {
  const row = page.getByTestId('file-row').filter({ hasText: fileName })
  await row.getByTestId('file-row-actions').click()
  await page.getByTestId('file-delete-action').click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toBeVisible()
  await confirm.getByTestId('file-delete-confirm').click()
  await expect(row).toHaveCount(0)
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('tianshu.lang', 'en-US')
  })
})

test('uploads, previews, downloads and deletes an image', async ({ page }) => {
  await signIn(page)
  await openFilesPage(page)

  const fileName = `e2e-image-${Date.now()}.png`
  await uploadFile(page, {
    name: fileName,
    mimeType: 'image/png',
    buffer: TINY_PNG,
  })

  const row = page.getByTestId('file-row').filter({ hasText: fileName })
  await expect(row).toBeVisible()
  await row.click()

  const sheet = page.getByRole('dialog')
  await expect(sheet.getByTestId('file-detail-sheet-title')).toBeVisible()
  const preview = sheet.locator('img').first()
  await expect(preview).toBeVisible()
  await expect
    .poll(() => preview.evaluate((img: HTMLImageElement) => img.naturalWidth))
    .toBeGreaterThan(0)

  const dlBtn = sheet.getByTestId('file-detail-download')
  await expect(dlBtn).toBeInViewport()
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    dlBtn.click(),
  ])
  expect(download.suggestedFilename()).toBe(fileName)

  await page.keyboard.press('Escape')
  await expect(sheet).toBeHidden()
  await deleteFileRow(page, fileName)
})

test('uploads a document with forced attachment download', async ({ page }) => {
  await signIn(page)
  await openFilesPage(page)

  const fileName = `e2e-doc-${Date.now()}.txt`
  await uploadFile(page, {
    name: fileName,
    mimeType: 'text/plain',
    buffer: Buffer.from('tianshu e2e document'),
  })

  const row = page.getByTestId('file-row').filter({ hasText: fileName })
  await expect(row).toBeVisible()
  await row.click()

  const sheet = page.getByRole('dialog')
  await expect(sheet.getByTestId('file-document-notice')).toBeVisible()

  const dlBtn = sheet.getByTestId('file-detail-download')
  await expect(dlBtn).toBeInViewport()
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    dlBtn.click(),
  ])
  expect(download.suggestedFilename()).toBe(fileName)

  await page.keyboard.press('Escape')
  await expect(sheet).toBeHidden()
  await deleteFileRow(page, fileName)
})
