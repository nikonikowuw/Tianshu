import { useMemo } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Logo } from '@/assets/logo'
import { normalizeApiError } from '@/lib/api/error'
import { setupStatusQueryOptions } from '@/lib/api/setup'
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
import { useInitSetupMutation } from './api'

type SetupWizardProps = {
  requireSetupTokenOverride?: boolean
  onSuccess?: () => void
}

const SERVER_FORM_FIELDS = new Set([
  'setupToken',
  'username',
  'email',
  'displayName',
  'password',
])

export function SetupWizard({
  requireSetupTokenOverride,
  onSuccess,
}: SetupWizardProps) {
  const { t } = useTranslation(['auth', 'common', 'validation'])
  const navigate = useNavigate()
  const statusQuery = useQuery({
    ...setupStatusQueryOptions,
    enabled: requireSetupTokenOverride === undefined,
  })
  const initMutation = useInitSetupMutation()

  const requireSetupToken =
    requireSetupTokenOverride ?? statusQuery.data?.requiresToken ?? true

  const schema = useMemo(
    () =>
      z
        .object({
          setupToken: requireSetupToken
            ? z.string().trim().min(1, t('validation:required'))
            : z.string().optional(),
          username: z.string().trim().min(3, t('validation:usernameMin')),
          email: z.string().trim().email(t('validation:invalidEmail')),
          displayName: z.string().trim().min(1, t('validation:required')),
          password: z.string().min(12, t('validation:passwordMin')),
          confirmPassword: z.string().min(1, t('validation:required')),
        })
        .refine((v) => v.password === v.confirmPassword, {
          path: ['confirmPassword'],
          message: t('validation:passwordMismatch'),
        }),
    [requireSetupToken, t]
  )

  type SetupFormValues = z.infer<typeof schema>

  const form = useForm<SetupFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      setupToken: '',
      username: '',
      email: '',
      displayName: '',
      password: '',
      confirmPassword: '',
    },
  })

  const onSubmit = async (values: SetupFormValues) => {
    try {
      await initMutation.mutateAsync({
        setupToken: requireSetupToken ? values.setupToken?.trim() : undefined,
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
      handleServerError(apiErr)
    }
  }

  return (
    <div className='container grid h-svh max-w-none items-center justify-center'>
      <div className='mx-auto flex w-full flex-col justify-center space-y-2 py-8 sm:w-[480px] sm:p-8'>
        <div className='mb-4 flex items-center justify-between'>
          <div className='flex items-center gap-2'>
            <Logo className='size-6' />
            <h1 className='text-xl font-medium'>{t('common:appName')}</h1>
          </div>
          <div className='flex items-center gap-1'>
            <LanguageSwitch />
            <ThemeSwitch />
          </div>
        </div>
        <Card className='gap-4'>
          <CardHeader>
            <CardTitle className='text-lg tracking-tight'>
              {t('auth:setup.title')}
            </CardTitle>
            <CardDescription>{t('auth:setup.description')}</CardDescription>
          </CardHeader>
          <CardContent>
            {!requireSetupToken && (
              <p className='mb-4 text-sm text-muted-foreground'>
                {t('auth:setup.localModeHint')}
              </p>
            )}
            <Form {...form}>
              <form
                onSubmit={form.handleSubmit(onSubmit)}
                className='grid gap-4'
                noValidate
              >
                {requireSetupToken && (
                  <FormField
                    control={form.control}
                    name='setupToken'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('auth:setup.setupToken')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder={t('auth:setup.setupTokenPlaceholder')}
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}
                <FormField
                  control={form.control}
                  name='username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('auth:setup.username')}</FormLabel>
                      <FormControl>
                        <Input autoComplete='username' {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='email'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('auth:setup.email')}</FormLabel>
                      <FormControl>
                        <Input type='email' autoComplete='email' {...field} />
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
                        <Input {...field} />
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
                      <FormLabel>{t('auth:setup.password')}</FormLabel>
                      <FormControl>
                        <PasswordInput autoComplete='new-password' {...field} />
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
                        <PasswordInput autoComplete='new-password' {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <Button
                  type='submit'
                  className='mt-2 w-full'
                  disabled={
                    initMutation.isPending ||
                    (requireSetupTokenOverride === undefined &&
                      statusQuery.isLoading)
                  }
                >
                  {initMutation.isPending
                    ? t('auth:setup.submitting')
                    : t('auth:setup.submit')}
                </Button>
              </form>
            </Form>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
