import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { AuthAlert } from '@/components/shared/auth-alert'
import { PasswordInput } from '@/components/shared/password-input'
import { useUpdateUserMutation } from '../queries'
import {
  resetPasswordFormSchema,
  type ResetPasswordFormValues,
} from '../schema'
import type { UserItem } from '../types'

type UserResetPasswordDialogProps = {
  user: UserItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UserResetPasswordDialog({
  user,
  open,
  onOpenChange,
}: UserResetPasswordDialogProps) {
  const { t } = useTranslation('users')
  const { t: tCommon } = useTranslation('common')
  const { t: tValidation } = useTranslation('validation')
  const [bannerError, setBannerError] = useState<string | null>(null)
  const [prevOpen, setPrevOpen] = useState(open)

  if (open !== prevOpen) {
    setPrevOpen(open)
    if (!open) {
      setBannerError(null)
    }
  }
  const mutation = useUpdateUserMutation()

  const schema = useMemo(
    () => resetPasswordFormSchema(tValidation),
    [tValidation]
  )

  const form = useForm<ResetPasswordFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      password: '',
      confirmPassword: '',
    },
  })

  useEffect(() => {
    if (!open) {
      form.reset({ password: '', confirmPassword: '' })
    }
  }, [open, form])

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && mutation.isPending) return
    if (!nextOpen) setBannerError(null)
    onOpenChange(nextOpen)
  }

  const onSubmit = async (values: ResetPasswordFormValues) => {
    if (!user) return
    setBannerError(null)
    try {
      await mutation.mutateAsync({
        id: user.id,
        payload: { password: values.password },
      })
      toast.success(t('toast.passwordReset'))
      handleOpenChange(false)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 422 && apiErr.fieldErrors?.password) {
        form.setError('password', { message: apiErr.fieldErrors.password })
        return
      }
      setBannerError(apiErr.message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('resetPasswordDialog.title')}</DialogTitle>
          <DialogDescription>
            {user
              ? t('resetPasswordDialog.description', {
                  username: user.username,
                })
              : ''}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((v) => void onSubmit(v))}
            onChange={() => {
              if (bannerError) setBannerError(null)
            }}
            className='space-y-4'
          >
            {bannerError && (
              <AuthAlert tone='destructive' title={bannerError} />
            )}

            <FormField
              control={form.control}
              name='password'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('resetPasswordDialog.newPassword')}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      {...field}
                      disabled={mutation.isPending}
                      autoComplete='new-password'
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
                  <FormLabel>
                    {t('resetPasswordDialog.confirmPassword')}
                  </FormLabel>
                  <FormControl>
                    <PasswordInput
                      {...field}
                      disabled={mutation.isPending}
                      autoComplete='new-password'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                disabled={mutation.isPending}
                onClick={() => handleOpenChange(false)}
              >
                {tCommon('cancel')}
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending && (
                  <Loader2 className='size-4 animate-spin' />
                )}
                {t('resetPasswordDialog.confirm')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
