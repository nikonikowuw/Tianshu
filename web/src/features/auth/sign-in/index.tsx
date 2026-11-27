import { useEffect, useMemo, useState } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { normalizeApiError } from '@/lib/api/error'
import { sanitizeRedirect } from '@/lib/auth/gateway'
import { handleServerError } from '@/lib/handle-server-error'
import { runeLength } from '@/lib/utils'
import { useRetryCountdown } from '@/hooks/use-retry-countdown'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { AuthShell } from '@/components/layout/auth-shell'
import { AuthAlert, RateLimitAlert } from '@/components/shared/auth-alert'
import { PasswordInput } from '@/components/shared/password-input'
import { useLoginMutation } from './api'

type SignInProps = {
  redirectTo?: string
  onSuccess?: () => void
}

const SERVER_FORM_FIELDS = new Set(['identifier', 'password'])

/** Localized inline failure that is not tied to a single form field. */
type FormError = {
  titleKey: 'errors:loginFailedTitle'
  description?: string
}

export function SignIn({ redirectTo, onSuccess }: SignInProps) {
  const { t } = useTranslation(['auth', 'validation', 'errors'])
  const navigate = useNavigate()
  const loginMutation = useLoginMutation()
  const cooldown = useRetryCountdown()
  const [formError, setFormError] = useState<FormError | null>(null)

  const schema = useMemo(
    () =>
      z.object({
        identifier: z
          .string()
          .trim()
          .refine((v) => runeLength(v) >= 1, {
            message: t('validation:required'),
          })
          .refine((v) => runeLength(v) <= 255, {
            message: t('validation:identifierMax'),
          }),
        password: z
          .string()
          .refine((v) => runeLength(v) >= 1, {
            message: t('validation:required'),
          })
          .refine((v) => runeLength(v) <= 128, {
            message: t('validation:passwordMax'),
          }),
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

  // Editing any field invalidates a stale failure banner (set once at mount).
  useEffect(() => {
    return form.subscribe({
      formState: { values: true },
      callback: () => setFormError(null),
    })
  }, [form])

  const onSubmit = async (values: SignInFormValues) => {
    setFormError(null)
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

      // 429: keep the failure visible and lock the button for the retry window.
      if (apiErr.status === 429) {
        cooldown.start(apiErr.retryAfter)
        return
      }

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

      if (apiErr.status === 401 || apiErr.status === 403) {
        setFormError({
          titleKey: 'errors:loginFailedTitle',
          description: apiErr.message,
        })
        return
      }

      handleServerError(apiErr)
    }
  }

  const isSubmitting = loginMutation.isPending
  const isLocked = isSubmitting || cooldown.isCoolingDown

  return (
    <AuthShell
      title={t('auth:signIn.title')}
      description={t('auth:signIn.description')}
      headerAlign='center'
      maxWidthClass='max-w-[400px]'
      belowCard={<p>{t('auth:signIn.footerHint')}</p>}
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className='grid gap-4'
          noValidate
        >
          {cooldown.isCoolingDown && (
            <RateLimitAlert seconds={cooldown.remaining} />
          )}
          {!cooldown.isCoolingDown && formError && (
            <AuthAlert
              tone='destructive'
              title={t(formError.titleKey)}
              description={formError.description}
            />
          )}

          <FormField
            control={form.control}
            name='identifier'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('auth:signIn.identifier')}</FormLabel>
                <FormControl>
                  <Input
                    data-testid='sign-in-identifier'
                    className='h-10'
                    autoComplete='username'
                    autoCapitalize='none'
                    spellCheck={false}
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
                    data-testid='sign-in-password'
                    inputClassName='h-10'
                    autoComplete='current-password'
                    placeholder={t('auth:signIn.passwordPlaceholder')}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button
            type='submit'
            data-testid='sign-in-submit'
            size='lg'
            className='mt-2 h-10 w-full font-medium'
            disabled={isLocked}
            aria-busy={isSubmitting}
          >
            {isSubmitting ? (
              <>
                <Loader2 className='size-4 animate-spin' />
                {t('auth:signIn.submitting')}
              </>
            ) : (
              t('auth:signIn.submit')
            )}
          </Button>
        </form>
      </Form>
    </AuthShell>
  )
}
