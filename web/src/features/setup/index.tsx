import { useEffect, useMemo, useState } from 'react'
import { z } from 'zod'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
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
import { PasswordStrength } from '@/components/shared/password-strength-meter'
import { useInitSetupMutation } from './api'

type SetupWizardProps = {
  onSuccess?: () => void
}

const SERVER_FORM_FIELDS = new Set([
  'username',
  'email',
  'displayName',
  'password',
])

type FormError = {
  titleKey: 'errors:setupFailedTitle'
  description?: string
}

export function SetupWizard({ onSuccess }: SetupWizardProps) {
  const { t } = useTranslation(['auth', 'validation', 'errors'])
  const navigate = useNavigate()
  const initMutation = useInitSetupMutation()
  const cooldown = useRetryCountdown()
  const [formError, setFormError] = useState<FormError | null>(null)

  const schema = useMemo(
    () =>
      z
        .object({
          username: z
            .string()
            .trim()
            .refine((v) => runeLength(v) >= 3, {
              message: t('validation:usernameMin'),
            })
            .refine((v) => runeLength(v) <= 64, {
              message: t('validation:usernameMax'),
            }),
          email: z
            .string()
            .trim()
            .email(t('validation:invalidEmail'))
            .refine((v) => runeLength(v) <= 255, {
              message: t('validation:emailMax'),
            }),
          displayName: z
            .string()
            .trim()
            .refine((v) => runeLength(v) >= 1, {
              message: t('validation:required'),
            })
            .refine((v) => runeLength(v) <= 100, {
              message: t('validation:displayNameMax'),
            }),
          password: z
            .string()
            .refine((v) => runeLength(v) >= 8, {
              message: t('validation:passwordMin'),
            })
            .refine((v) => runeLength(v) <= 128, {
              message: t('validation:passwordMax'),
            }),
          confirmPassword: z.string().min(1, t('validation:required')),
        })
        .refine((v) => v.password === v.confirmPassword, {
          path: ['confirmPassword'],
          message: t('validation:passwordMismatch'),
        }),
    [t]
  )

  type SetupFormValues = z.infer<typeof schema>

  const form = useForm<SetupFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      username: '',
      email: '',
      displayName: '',
      password: '',
      confirmPassword: '',
    },
  })

  const passwordValue = useWatch({ control: form.control, name: 'password' })

  // Editing any field invalidates a stale failure banner (set once at mount).
  useEffect(() => {
    return form.subscribe({
      formState: { values: true },
      callback: () => setFormError(null),
    })
  }, [form])

  // Keep the confirm field in sync: changing the password must re-validate a
  // previously typed confirmation instead of leaving a stale mismatch error.
  useEffect(() => {
    const confirmState = form.getFieldState('confirmPassword')
    if (
      confirmState.isTouched ||
      confirmState.isDirty ||
      Boolean(confirmState.error) ||
      form.getValues('confirmPassword') !== ''
    ) {
      void form.trigger('confirmPassword')
    }
  }, [passwordValue, form])

  const onSubmit = async (values: SetupFormValues) => {
    setFormError(null)
    try {
      await initMutation.mutateAsync({
        username: values.username.trim(),
        email: values.email.trim(),
        displayName: values.displayName.trim(),
        password: values.password,
      })
      toast.success(t('auth:setup.success'))
      if (onSuccess) {
        onSuccess()
        return
      }
      await navigate({ to: '/sign-in', replace: true })
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
            form.setError(field as keyof SetupFormValues, { message })
            mapped = true
          }
        }
        if (mapped) return
      }

      if (
        apiErr.status === 409 ||
        apiErr.status === 401 ||
        apiErr.status === 403
      ) {
        setFormError({
          titleKey: 'errors:setupFailedTitle',
          description: apiErr.message,
        })
        return
      }

      handleServerError(apiErr)
    }
  }

  const isSubmitting = initMutation.isPending
  const isLocked = isSubmitting || cooldown.isCoolingDown

  return (
    <AuthShell
      eyebrow={t('auth:setup.eyebrow')}
      title={t('auth:setup.title')}
      description={t('auth:setup.description')}
      headerAlign='start'
      maxWidthClass='max-w-[480px]'
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className='grid gap-5'
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

          {/* Account details */}
          <fieldset className='grid gap-4'>
            <legend className='mb-3 text-xs font-medium tracking-wide text-muted-foreground'>
              {t('auth:setup.sectionAccount')}
            </legend>

            <div className='grid gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='username'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('auth:setup.username')}</FormLabel>
                    <FormControl>
                      <Input
                        className='h-10'
                        autoComplete='username'
                        autoCapitalize='none'
                        spellCheck={false}
                        placeholder={t('auth:setup.usernamePlaceholder')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='displayName'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('auth:setup.displayName')}</FormLabel>
                    <FormControl>
                      <Input
                        className='h-10'
                        autoComplete='name'
                        placeholder={t('auth:setup.displayNamePlaceholder')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='email'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('auth:setup.email')}</FormLabel>
                  <FormControl>
                    <Input
                      className='h-10'
                      type='email'
                      autoComplete='email'
                      autoCapitalize='none'
                      spellCheck={false}
                      placeholder={t('auth:setup.emailPlaceholder')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </fieldset>

          <div className='h-px bg-border/70' aria-hidden='true' />

          {/* Security credentials */}
          <fieldset className='grid gap-4'>
            <legend className='mb-3 text-xs font-medium tracking-wide text-muted-foreground'>
              {t('auth:setup.sectionSecurity')}
            </legend>

            <div className='grid gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('auth:setup.password')}</FormLabel>
                    <FormControl>
                      <PasswordInput
                        inputClassName='h-10'
                        autoComplete='new-password'
                        placeholder={t('auth:setup.passwordPlaceholder')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='confirmPassword'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('auth:setup.confirmPassword')}</FormLabel>
                    <FormControl>
                      <PasswordInput
                        inputClassName='h-10'
                        autoComplete='new-password'
                        placeholder={t('auth:setup.confirmPasswordPlaceholder')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='grid gap-2'>
              <PasswordStrength value={passwordValue} />
              <p className='text-xs text-muted-foreground'>
                {t('auth:setup.passwordHint')}
              </p>
            </div>
          </fieldset>

          <AuthAlert
            tone='warning'
            live='off'
            title={t('auth:setup.irreversibleTitle')}
            description={t('auth:setup.irreversibleDesc')}
          />

          <Button
            type='submit'
            size='lg'
            className='h-10 w-full font-medium'
            disabled={isLocked}
            aria-busy={isSubmitting}
          >
            {isSubmitting ? (
              <>
                <Loader2 className='size-4 animate-spin' />
                {t('auth:setup.submitting')}
              </>
            ) : (
              t('auth:setup.submit')
            )}
          </Button>
        </form>
      </Form>
    </AuthShell>
  )
}
