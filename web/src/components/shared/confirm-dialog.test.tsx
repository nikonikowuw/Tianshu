import type { ReactNode, SubmitEvent } from 'react'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n from '@/i18n/index'
import { ConfirmDialog } from './confirm-dialog'

const CANCEL_RE = /cancel|取消/i
const CONFIRM_RE = /confirm|确认/i

function renderWithI18n(ui: ReactNode) {
  return render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>)
}

describe('ConfirmDialog', () => {
  it('renders title, description, and localized default buttons', async () => {
    const { getByRole, getByText } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Delete item'
        desc='This action cannot be undone.'
        handleConfirm={vi.fn()}
      />
    )

    await expect
      .element(getByRole('heading', { name: 'Delete item' }))
      .toBeInTheDocument()
    await expect
      .element(getByText('This action cannot be undone.'))
      .toBeInTheDocument()
    await expect
      .element(getByRole('button', { name: CANCEL_RE }))
      .toBeInTheDocument()
    await expect
      .element(getByRole('button', { name: CONFIRM_RE }))
      .toBeInTheDocument()
  })

  it('calls handleConfirm when the confirm button is clicked', async () => {
    const handleConfirm = vi.fn()
    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Sign out'
        desc='Are you sure?'
        confirmText='Sign out'
        handleConfirm={handleConfirm}
      />
    )

    await userEvent.click(getByRole('button', { name: 'Sign out' }))
    expect(handleConfirm).toHaveBeenCalledOnce()
  })

  it('disables confirm when disabled is true', async () => {
    const handleConfirm = vi.fn()
    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Danger'
        desc='...'
        disabled
        handleConfirm={handleConfirm}
      />
    )

    const confirm = getByRole('button', { name: CONFIRM_RE })
    await expect.element(confirm).toBeDisabled()
    expect(handleConfirm).not.toHaveBeenCalled()
  })

  it('when isLoading is true, disables cancel/confirm and blocks Escape dismissal', async () => {
    const handleConfirm = vi.fn()
    const onOpenChange = vi.fn()
    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={onOpenChange}
        title='Loading'
        desc='...'
        isLoading
        handleConfirm={handleConfirm}
      />
    )

    await expect
      .element(getByRole('button', { name: CANCEL_RE }))
      .toBeDisabled()
    await expect
      .element(getByRole('button', { name: CONFIRM_RE }))
      .toBeDisabled()

    await userEvent.keyboard('{Escape}')
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('supports custom button texts', async () => {
    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Delete'
        desc='...'
        cancelBtnText='No'
        confirmText='Yes'
        handleConfirm={vi.fn()}
      />
    )

    await expect
      .element(getByRole('button', { name: 'No' }))
      .toBeInTheDocument()
    await expect
      .element(getByRole('button', { name: 'Yes' }))
      .toBeInTheDocument()
  })

  it('renders confirm as submit button linked to desc form when `form` is set', async () => {
    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Delete tasks'
        form='tasks-multi-delete-form'
        desc={
          <form id='tasks-multi-delete-form' className='space-y-4'>
            <p>Type DELETE to confirm.</p>
          </form>
        }
        confirmText='Delete'
        destructive
      />
    )

    const deleteBtn = getByRole('button', { name: 'Delete' })
    await expect.element(deleteBtn).toHaveAttribute('type', 'submit')
    await expect
      .element(deleteBtn)
      .toHaveAttribute('form', 'tasks-multi-delete-form')
  })

  it('submits the desc form when confirm is clicked (form prop, no handleConfirm)', async () => {
    const handleFormSubmit = vi.fn((e: SubmitEvent<HTMLFormElement>) => {
      e.preventDefault()
    })

    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Delete'
        form='users-delete-form'
        desc={
          <form
            id='users-delete-form'
            onSubmit={handleFormSubmit}
            className='space-y-4'
          >
            <p>Confirm deletion.</p>
          </form>
        }
        confirmText='Delete'
        destructive
      />
    )

    await userEvent.click(getByRole('button', { name: 'Delete' }))

    expect(handleFormSubmit).toHaveBeenCalledOnce()
  })

  it('submits the form when Enter key is pressed', async () => {
    const handleFormSubmit = vi.fn((e: SubmitEvent<HTMLFormElement>) => {
      e.preventDefault()
    })

    const { getByPlaceholder } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Delete'
        form='users-delete-form'
        desc={
          <form
            id='users-delete-form'
            onSubmit={handleFormSubmit}
            className='space-y-4'
          >
            <input type='text' name='username' placeholder='username' />
          </form>
        }
        confirmText='Delete'
        destructive
      />
    )

    await userEvent.fill(getByPlaceholder('username'), 'test')
    await userEvent.keyboard('{Enter}')
    expect(handleFormSubmit).toHaveBeenCalledOnce()
  })

  it('does not submit the form when confirm is disabled (typed confirmation mismatch)', async () => {
    const handleFormSubmit = vi.fn((e: SubmitEvent<HTMLFormElement>) => {
      e.preventDefault()
    })

    const { getByRole } = await renderWithI18n(
      <ConfirmDialog
        open
        onOpenChange={vi.fn()}
        title='Delete'
        form='users-delete-form'
        disabled
        desc={
          <form id='users-delete-form' onSubmit={handleFormSubmit}>
            <p>Enter username to enable Delete.</p>
          </form>
        }
        confirmText='Delete'
        destructive
      />
    )

    const deleteBtn = getByRole('button', { name: 'Delete' })
    await expect.element(deleteBtn).toBeDisabled()
    expect(handleFormSubmit).not.toHaveBeenCalled()
  })
})
