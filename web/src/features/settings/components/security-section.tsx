import { useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { useRetryCountdown } from '@/hooks/use-retry-countdown'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { AuthAlert, RateLimitAlert } from '@/components/shared/auth-alert'
import { PasswordInput } from '@/components/shared/password-input'
import { PasswordStrength } from '@/components/shared/password-strength-meter'
import { useChangePasswordMutation } from '../queries'
import {
  changePasswordFormSchema,
  type ChangePasswordFormValues,
} from '../schema'
import { ContentSection } from './content-section'

const DEFAULT_VALUES: ChangePasswordFormValues = {
  currentPassword: '',
  newPassword: '',
  confirmPassword: '',
  revokeOtherSessions: true,
}

export function SecuritySection() {
  const { t } = useTranslation('settings')
  const { t: tValidation } = useTranslation('validation')
  const [bannerError, setBannerError] = useState<string | null>(null)
  const cooldown = useRetryCountdown()
  const changePasswordMutation = useChangePasswordMutation()

  const schema = useMemo(
    () => changePasswordFormSchema(tValidation),
    [tValidation]
  )

  const form = useForm<ChangePasswordFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_VALUES,
  })

  const newPasswordValue = useWatch({
    control: form.control,
    name: 'newPassword',
  })

  useEffect(() => {
    return form.subscribe({
      formState: { values: true },
      callback: () => setBannerError(null),
    })
  }, [form])

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
  }, [newPasswordValue, form])

  const onSubmit = async (values: ChangePasswordFormValues) => {
    setBannerError(null)
    try {
      const res = await changePasswordMutation.mutateAsync({
        currentPassword: values.currentPassword,
        newPassword: values.newPassword,
        revokeOtherSessions: values.revokeOtherSessions,
      })
      form.reset(DEFAULT_VALUES)
      if (res.revokedSessions > 0) {
        toast.success(
          t('security.updatedWithRevoked', { count: res.revokedSessions })
        )
      } else {
        toast.success(t('security.updated'))
      }
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 401 || apiErr.code === 'AUTH_ACCOUNT_DISABLED') {
        return
      }
      if (apiErr.status === 429) {
        cooldown.start(apiErr.retryAfter)
        return
      }
      if (apiErr.code === 'PROFILE_INVALID_CURRENT_PASSWORD') {
        form.setError('currentPassword', { message: apiErr.message })
        return
      }
      if (apiErr.status === 422 && Object.keys(apiErr.fieldErrors).length > 0) {
        let mapped = false
        for (const [field, message] of Object.entries(apiErr.fieldErrors)) {
          if (field === 'currentPassword' || field === 'newPassword') {
            form.setError(field, { message })
            mapped = true
          }
        }
        if (mapped) return
      }
      setBannerError(formatServerErrorMessage(apiErr))
    }
  }

  const isSubmitting = changePasswordMutation.isPending
  const isLocked = isSubmitting || cooldown.isCoolingDown

  return (
    <ContentSection>
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='flex flex-row items-start gap-3.5 p-6 pb-4'>
          <div className='flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted/50 text-foreground'>
            <ShieldCheck className='size-5' />
          </div>
          <div className='space-y-1'>
            <CardTitle className='text-base'>{t('security.title')}</CardTitle>
            <CardDescription>{t('security.description')}</CardDescription>
          </div>
        </CardHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((v) => void onSubmit(v))}
            noValidate
          >
            <CardContent className='space-y-5 px-6 pb-6'>
              {cooldown.isCoolingDown && (
                <RateLimitAlert seconds={cooldown.remaining} />
              )}
              {!cooldown.isCoolingDown && bannerError && (
                <AuthAlert tone='destructive' title={bannerError} />
              )}

              <div className='sm:max-w-md'>
                <FormField
                  control={form.control}
                  name='currentPassword'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('security.currentPassword')}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          {...field}
                          data-testid='security-current-password'
                          disabled={isSubmitting}
                          autoComplete='current-password'
                          placeholder={t('security.currentPasswordPlaceholder')}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <Separator />

              <div className='grid grid-cols-1 gap-5 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='newPassword'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('security.newPassword')}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          {...field}
                          data-testid='security-new-password'
                          disabled={isSubmitting}
                          autoComplete='new-password'
                          placeholder={t('security.newPasswordPlaceholder')}
                        />
                      </FormControl>
                      <PasswordStrength
                        value={newPasswordValue}
                        className='pt-1'
                      />
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='confirmPassword'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('security.confirmPassword')}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          {...field}
                          data-testid='security-confirm-password'
                          disabled={isSubmitting}
                          autoComplete='new-password'
                          placeholder={t('security.confirmPasswordPlaceholder')}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <FormField
                control={form.control}
                name='revokeOtherSessions'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-4 rounded-lg border bg-muted/25 px-4 py-3.5'>
                    <div className='space-y-0.5'>
                      <FormLabel>{t('security.revokeOtherSessions')}</FormLabel>
                      <FormDescription>
                        {t('security.revokeOtherSessionsHint')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        data-testid='security-revoke-others'
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={isSubmitting}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </CardContent>

            <CardFooter className='flex flex-wrap items-center justify-between gap-3 border-t bg-muted/30 px-6 py-3.5 [.border-t]:pt-3.5'>
              <p className='text-xs text-muted-foreground'>
                {t('security.newPasswordHint')}
              </p>
              <Button
                type='submit'
                size='sm'
                data-testid='security-submit'
                disabled={isLocked}
                aria-busy={isSubmitting}
              >
                {isSubmitting && <Loader2 className='size-3.5 animate-spin' />}
                {isSubmitting ? t('security.submitting') : t('security.submit')}
              </Button>
            </CardFooter>
          </form>
        </Form>
      </Card>
    </ContentSection>
  )
}
