import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n from '@/i18n/index'
import { SearchProvider } from '@/context/search-provider'
import { ThemeProvider } from '@/context/theme-provider'
import { SidebarProvider } from '@/components/ui/sidebar'
import { CommandMenu } from '@/components/layout/command-menu'
import { Main } from '@/components/layout/main'
import { NavGroup } from '@/components/layout/nav-group'
import { Search } from './search'
import { SkipToMain } from './skip-to-main'
import { ThemeSwitch } from './theme-switch'

let mockHref = '/users?page=2'

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
  useLocation: (opts?: { select?: (loc: { href: string }) => unknown }) =>
    opts?.select ? opts.select({ href: mockHref }) : { href: mockHref },
  useNavigate: () => vi.fn(),
}))

describe('Shell & Accessibility Controls', () => {
  it('links SkipToMain #content anchor directly to Main element with localized text', async () => {
    const { getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <SkipToMain />
        <Main>
          <h1>Heading</h1>
        </Main>
      </I18nextProvider>
    )

    const skipLink = getByRole('link', {
      name: /跳到主内容|Skip to main content/i,
    })
    await expect.element(skipLink).toHaveAttribute('href', '#content')
    const mainEl = getByRole('main').element()
    expect(mainEl.id).toBe('content')
    expect(mainEl.getAttribute('tabindex')).toBe('-1')
  })

  it('syncs meta theme-color using resolvedTheme and localizes ThemeSwitch labels', async () => {
    const meta = document.createElement('meta')
    meta.name = 'theme-color'
    document.head.appendChild(meta)

    try {
      const { getByRole } = await render(
        <I18nextProvider i18n={i18n}>
          <ThemeProvider defaultTheme='dark' storageKey='test-theme-key'>
            <ThemeSwitch />
          </ThemeProvider>
        </I18nextProvider>
      )

      await vi.waitFor(() => {
        expect(meta.getAttribute('content')).toBe('#020817')
      })

      const trigger = getByRole('button', {
        name: /切换主题|Toggle theme/i,
      })
      await userEvent.click(trigger)
      await expect
        .element(getByRole('menuitem', { name: /浅色|Light/i }))
        .toBeVisible()
      await expect
        .element(getByRole('menuitem', { name: /深色|Dark/i }))
        .toBeVisible()
      await expect
        .element(getByRole('menuitem', { name: /跟随系统|System/i }))
        .toBeVisible()
    } finally {
      meta.remove()
    }
  })

  it('localizes Search trigger and CommandMenu dialog accessible title/description', async () => {
    const { getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <ThemeProvider defaultTheme='light' storageKey='test-cmd-theme'>
          <SearchProvider>
            <Search />
            <CommandMenu navGroups={[]} />
          </SearchProvider>
        </ThemeProvider>
      </I18nextProvider>
    )

    const trigger = getByRole('button', { name: /搜索|Search/i })
    await userEvent.click(trigger)

    await expect
      .element(getByRole('dialog', { name: /命令面板|Command Palette/i }))
      .toBeVisible()
  })

  it('matches collapsible NavGroup child routes even when URL carries query params', async () => {
    mockHref = '/users?page=2'
    const { getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <SidebarProvider defaultOpen>
          <NavGroup
            title='System'
            items={[
              {
                title: 'Access',
                items: [{ title: 'Users', url: '/users' }],
              },
            ]}
          />
        </SidebarProvider>
      </I18nextProvider>
    )

    const childLink = getByRole('link', { name: 'Users' })
    await expect.element(childLink).toBeVisible()
    await expect.element(childLink).toHaveAttribute('data-active', 'true')
  })
})
