import { useForm } from 'react-hook-form'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n from '@/i18n/index'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { PasswordInput } from './password-input'

const SHOW_PASSWORD_RE = /show password|显示密码/i
const HIDE_PASSWORD_RE = /hide password|隐藏密码/i

describe('PasswordInput', () => {
  it('renders the password input correctly', async () => {
    const { getByPlaceholder, getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <PasswordInput placeholder='password' />
      </I18nextProvider>
    )

    const passwordInput = getByPlaceholder('password')
    const showPasswordButton = getByRole('button', { name: SHOW_PASSWORD_RE })

    await expect.element(passwordInput).toBeInTheDocument()
    await expect.element(passwordInput).toHaveAttribute('type', 'password')
    await expect.element(showPasswordButton).toBeVisible()
    await expect
      .element(showPasswordButton)
      .not.toHaveAttribute('tabindex', '-1')
    await expect
      .element(showPasswordButton)
      .toHaveAttribute('aria-pressed', 'false')
  })

  it('toggles the password visibility when the show password button is clicked', async () => {
    const { getByPlaceholder, getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <PasswordInput placeholder='password' />
      </I18nextProvider>
    )

    const passwordInput = getByPlaceholder('password')
    const showPasswordButton = getByRole('button', { name: SHOW_PASSWORD_RE })

    await expect.element(passwordInput).toHaveAttribute('type', 'password')
    await expect.element(showPasswordButton).toBeInTheDocument()

    await userEvent.click(showPasswordButton)

    await expect.element(passwordInput).toHaveAttribute('type', 'text')
    const hidePasswordButton = getByRole('button', { name: HIDE_PASSWORD_RE })
    await expect.element(hidePasswordButton).toBeInTheDocument()
    await expect
      .element(hidePasswordButton)
      .toHaveAttribute('aria-pressed', 'true')

    await userEvent.click(hidePasswordButton)

    await expect.element(passwordInput).toHaveAttribute('type', 'password')
    await expect
      .element(getByRole('button', { name: SHOW_PASSWORD_RE }))
      .toBeInTheDocument()
  })

  it('disables the show password button when the password input is disabled', async () => {
    const { getByPlaceholder, getByRole } = await render(
      <I18nextProvider i18n={i18n}>
        <PasswordInput placeholder='password' disabled />
      </I18nextProvider>
    )

    const passwordInput = getByPlaceholder('password')
    const showPasswordButton = getByRole('button', { name: SHOW_PASSWORD_RE })
    await expect.element(showPasswordButton).toBeDisabled()
    await expect.element(passwordInput).toBeDisabled()
  })

  it('works with FormLabel and react-hook-form field spread', async () => {
    function PasswordInLabeledForm() {
      const form = useForm<{ password: string }>({
        defaultValues: { password: '' },
      })

      return (
        <I18nextProvider i18n={i18n}>
          <Form {...form}>
            <form>
              <FormField
                control={form.control}
                name='password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Password</FormLabel>
                    <FormControl>
                      <PasswordInput {...field} />
                    </FormControl>
                  </FormItem>
                )}
              />
            </form>
          </Form>
        </I18nextProvider>
      )
    }

    const { getByLabelText } = await render(<PasswordInLabeledForm />)

    const password = getByLabelText(/^Password$/i)
    await expect.element(password).toHaveAttribute('type', 'password')

    await userEvent.type(password, 'secret-value')

    await expect.element(password).toHaveValue('secret-value')
  })
})
