import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import '@/styles/index.css'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { page } from 'vitest/browser'
import i18n from '@/i18n/index'
import { LayoutProvider } from '@/context/layout-provider'
import { SidebarProvider } from '@/components/ui/sidebar'
import { AppSidebar } from './app-sidebar'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    to,
    children,
    ...props
  }: {
    to: string
    children: ReactNode
    className?: string
  }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  useLocation: () => ({ href: '/' }),
}))

describe('AppSidebar', () => {
  it('keeps the brand logo badge a 32x32 square and preserves accessible name when collapsed', async () => {
    await page.viewport(1280, 720)
    const { getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <LayoutProvider>
          <SidebarProvider defaultOpen={false}>
            <AppSidebar
              appName='Custom Console'
              navGroups={[]}
              user={{ name: 'Admin', email: 'admin@example.com' }}
              onSignOut={() => {}}
            />
          </SidebarProvider>
        </LayoutProvider>
      </I18nextProvider>
    )

    const brandLink = getByRole('link', { name: 'Custom Console' })
    await expect.element(brandLink).toBeVisible()

    const linkEl = brandLink.element()
    const badgeEl = linkEl.firstElementChild as HTMLElement | null
    expect(badgeEl).not.toBeNull()

    const rect = badgeEl!.getBoundingClientRect()
    expect(Math.round(rect.width)).toBe(32)
    expect(Math.round(rect.height)).toBe(32)
  })

  it('renders brand name and subtitle with the brand font when expanded', async () => {
    await page.viewport(1280, 720)
    const { getByText } = await render(
      <I18nextProvider i18n={i18n}>
        <LayoutProvider>
          <SidebarProvider defaultOpen={true}>
            <AppSidebar
              appName='Tianshu'
              appSubtitle='天枢控制台'
              navGroups={[]}
              user={{ name: 'Admin', email: 'admin@example.com' }}
              onSignOut={() => {}}
            />
          </SidebarProvider>
        </LayoutProvider>
      </I18nextProvider>
    )

    const brandTitle = getByText('Tianshu')
    const brandSubtitle = getByText('天枢控制台')
    await expect.element(brandTitle).toBeVisible()
    await expect.element(brandSubtitle).toBeVisible()
    expect(window.getComputedStyle(brandTitle.element()).fontFamily).toContain(
      'Plus Jakarta Sans Variable'
    )
  })
})
