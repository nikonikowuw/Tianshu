import { useMemo } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from '@tanstack/react-router'
import { normalizeApiError } from '@/lib/api/error'
import { sanitizeRedirect } from '@/lib/auth/gateway'
import { handleServerError } from '@/lib/handle-server-error'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { PasswordInput } from '@/components/shared/password-input'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { AuthLayout } from '../auth-layout'
import { useLoginMutation } from './api'

type SignInProps = {
  redirectTo?: string
  onSuccess?: () => void
}

const SERVER_FORM_FIELDS = new Set(['identifier', 'password'])

export function SignIn({ redirectTo, onSuccess }: SignInProps) {
  const { t } = useTranslation(['auth', 'validation'])
  const navigate = useNavigate()
  const loginMutation = useLoginMutation()

  const schema = useMemo(
    () =>
      z.object({
        identifier: z.string().trim().min(1, t('validation:required')),
        password: z.string().min(1, t('validation:required')),
      }),
    [t]
  )

  type SignInFormValues = z.infer<typeof schema>

  const form = useForm<SignInFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      identifier: '',
      password: '',
    },
  })

  const onSubmit = async (values: SignInFormValues) => {
    try {
      await loginMutation.mutateAsync({
        identifier: values.identifier.trim(),
        password: values.password,
      })
      if (onSuccess) {
        onSuccess()
        return
      }
      const target = sanitizeRedirect(redirectTo)
      await navigate({ to: target, replace: true })
    } catch (error) {
      const apiErr = normalizeApiError(error)
      if (apiErr.status === 422) {
        let mapped = false
        for (const [field, message] of Object.entries(apiErr.fieldErrors)) {
          if (SERVER_FORM_FIELDS.has(field)) {
            form.setError(field as keyof SignInFormValues, { message })
            mapped = true
          }
        }
        if (mapped) return
      }
      handleServerError(apiErr)
    }
  }

  return (
    <AuthLayout>
      <div className='mb-3 flex justify-end gap-1'>
        <LanguageSwitch />
        <ThemeSwitch />
      </div>
      <Card className='gap-4'>
        <CardHeader>
          <CardTitle className='text-lg tracking-tight'>
            {t('auth:signIn.title')}
          </CardTitle>
          <CardDescription>{t('auth:signIn.description')}</CardDescription>
        </CardHeader>
        <CardContent>
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(onSubmit)}
              className='grid gap-4'
              noValidate
            >
              <FormField
                control={form.control}
                name='identifier'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('auth:signIn.identifier')}</FormLabel>
                    <FormControl>
                      <Input
                        autoComplete='username'
                        placeholder={t('auth:signIn.identifierPlaceholder')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('auth:signIn.password')}</FormLabel>
                    <FormControl>
                      <PasswordInput
                        autoComplete='current-password'
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button
                type='submit'
                className='mt-2 w-full'
                disabled={loginMutation.isPending}
              >
                {loginMutation.isPending
                  ? t('auth:signIn.submitting')
                  : t('auth:signIn.submit')}
              </Button>
            </form>
          </Form>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
