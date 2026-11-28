import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { useCurrentUser, useIsSuperAdmin } from '@/lib/auth/use-permission'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { AuthAlert } from '@/components/shared/auth-alert'
import { PasswordInput } from '@/components/shared/password-input'
import {
  roleOptionsQueryOptions,
  useCreateUserMutation,
  useUpdateUserMutation,
} from '../queries'
import {
  createUserFormSchema,
  updateUserFormSchema,
  type UserFormValues,
} from '../schema'
import type { UserItem } from '../types'
import { RoleMultiSelect } from './role-multi-select'

type UserFormSheetProps = {
  open: boolean
  mode: 'create' | 'edit'
  user: UserItem | null
  onOpenChange: (open: boolean) => void
}

const DEFAULT_USER_FORM_VALUES: UserFormValues = {
  username: '',
  email: '',
  displayName: '',
  password: '',
  status: 'active',
  roleIds: [],
}

export function UserFormSheet({
  open,
  mode,
  user,
  onOpenChange,
}: UserFormSheetProps) {
  const { t, i18n } = useTranslation('users')
  const { t: tCommon } = useTranslation('common')
  const { t: tValidation } = useTranslation('validation')
  const currentUser = useCurrentUser()
  const actorIsSuperAdmin = useIsSuperAdmin()
  const [bannerError, setBannerError] = useState<string | null>(null)
  const [prevOpen, setPrevOpen] = useState(open)

  if (open !== prevOpen) {
    setPrevOpen(open)
    if (!open) {
      setBannerError(null)
    }
  }

  const isEdit = mode === 'edit' && user !== null
  const isSelf = isEdit && currentUser?.id === user.id

  const {
    data: roleOptionsData,
    isLoading: isRoleOptionsLoading,
    isError: isRoleOptionsError,
    refetch: refetchRoleOptions,
  } = useQuery({
    ...roleOptionsQueryOptions(i18n.language),
    enabled: open,
  })
  const roleOptions = useMemo(
    () => roleOptionsData?.items ?? [],
    [roleOptionsData]
  )

  const schema = useMemo(
    () =>
      isEdit
        ? updateUserFormSchema(tValidation)
        : createUserFormSchema(tValidation),
    [isEdit, tValidation]
  )

  const form = useForm<UserFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_USER_FORM_VALUES,
  })

  useEffect(() => {
    if (!open) {
      form.reset(DEFAULT_USER_FORM_VALUES)
      return
    }
    if (isEdit) {
      form.reset({
        username: user.username,
        email: user.email,
        displayName: user.displayName,
        password: '',
        status: user.status,
        roleIds: user.roles.map((r) => r.id),
      })
    } else {
      form.reset(DEFAULT_USER_FORM_VALUES)
    }
  }, [open, isEdit, user, form])

  const createMutation = useCreateUserMutation()
  const updateMutation = useUpdateUserMutation()
  const isSubmitting = createMutation.isPending || updateMutation.isPending

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && isSubmitting) return
    if (!nextOpen) setBannerError(null)
    onOpenChange(nextOpen)
  }

  const onSubmit = async (values: UserFormValues) => {
    setBannerError(null)
    try {
      if (isEdit) {
        await updateMutation.mutateAsync({
          id: user.id,
          payload: {
            email: values.email.trim(),
            displayName: values.displayName.trim(),
            status: values.status,
            roleIds: values.roleIds,
          },
        })
        toast.success(t('toast.updated'))
      } else {
        await createMutation.mutateAsync({
          username: values.username.trim(),
          email: values.email.trim(),
          displayName: values.displayName.trim() || undefined,
          password: values.password,
          status: values.status,
          roleIds: values.roleIds,
        })
        toast.success(t('toast.created'))
      }
      onOpenChange(false)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.code === 'USERS_USERNAME_CONFLICT') {
        form.setError('username', { message: apiErr.message })
        return
      }
      if (apiErr.code === 'USERS_EMAIL_CONFLICT') {
        form.setError('email', { message: apiErr.message })
        return
      }
      if (
        apiErr.status === 422 &&
        apiErr.fieldErrors &&
        Object.keys(apiErr.fieldErrors).length > 0
      ) {
        let mapped = false
        for (const [field, message] of Object.entries(apiErr.fieldErrors)) {
          if (
            field === 'username' ||
            field === 'email' ||
            field === 'displayName' ||
            field === 'password' ||
            field === 'status' ||
            field === 'roleIds'
          ) {
            form.setError(field, { message })
            mapped = true
          }
        }
        if (mapped) return
      }
      setBannerError(formatServerErrorMessage(apiErr))
    }
  }

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent className='flex w-full flex-col gap-0 p-0 sm:max-w-lg'>
        <SheetHeader className='border-b px-6 py-4'>
          <SheetTitle>
            {isEdit ? t('form.editTitle') : t('form.createTitle')}
          </SheetTitle>
          <SheetDescription>
            {isEdit ? t('form.editDescription') : t('form.createDescription')}
          </SheetDescription>
        </SheetHeader>

        <Form {...form}>
          <form
            noValidate
            onSubmit={form.handleSubmit((v) => void onSubmit(v))}
            onChange={() => {
              if (bannerError) setBannerError(null)
            }}
            className='flex min-h-0 flex-1 flex-col'
          >
            <ScrollArea className='min-h-0 flex-1'>
              <div className='space-y-5 px-6 py-5'>
                {bannerError && (
                  <AuthAlert tone='destructive' title={bannerError} />
                )}

                <FormField
                  control={form.control}
                  name='username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.username')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          readOnly={isEdit}
                          disabled={isEdit || isSubmitting}
                          placeholder={t('form.usernamePlaceholder')}
                          autoComplete='off'
                        />
                      </FormControl>
                      {isEdit && (
                        <FormDescription>
                          {t('form.usernameImmutableHint')}
                        </FormDescription>
                      )}
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='email'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.email')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='email'
                          disabled={isSubmitting}
                          placeholder={t('form.emailPlaceholder')}
                          autoComplete='off'
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
                      <FormLabel>{t('form.displayName')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          disabled={isSubmitting}
                          placeholder={t('form.displayNamePlaceholder')}
                          autoComplete='off'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {!isEdit && (
                  <FormField
                    control={form.control}
                    name='password'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('form.password')}</FormLabel>
                        <FormControl>
                          <PasswordInput
                            {...field}
                            disabled={isSubmitting}
                            placeholder={t('form.passwordPlaceholder')}
                            autoComplete='new-password'
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='roleIds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.roles')}</FormLabel>
                      <FormControl>
                        <RoleMultiSelect
                          options={roleOptions}
                          value={field.value}
                          onChange={(next) => {
                            if (bannerError) setBannerError(null)
                            field.onChange(next)
                          }}
                          disabled={isSubmitting}
                          isLoading={isRoleOptionsLoading}
                          isError={isRoleOptionsError}
                          onRetry={() => void refetchRoleOptions()}
                          actorIsSuperAdmin={actorIsSuperAdmin}
                          isSelf={isSelf}
                        />
                      </FormControl>
                      {!actorIsSuperAdmin && (
                        <FormDescription>
                          {t('form.superAdminProtected')}
                        </FormDescription>
                      )}
                      {isSelf &&
                        user.roles.some((r) => r.code === 'super_admin') && (
                          <FormDescription>
                            {t('form.selfSuperAdminHint')}
                          </FormDescription>
                        )}
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='status'
                  render={({ field }) => (
                    <FormItem>
                      <div className='flex items-center justify-between rounded-lg border p-3'>
                        <div className='space-y-0.5'>
                          <FormLabel>{t('form.status')}</FormLabel>
                          <FormDescription>
                            {t('form.statusActiveHint')}
                          </FormDescription>
                        </div>
                        <FormControl>
                          <Switch
                            checked={field.value === 'active'}
                            onCheckedChange={(checked) => {
                              if (bannerError) setBannerError(null)
                              field.onChange(checked ? 'active' : 'disabled')
                            }}
                            disabled={isSubmitting || isSelf}
                          />
                        </FormControl>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </ScrollArea>

            <SheetFooter className='flex-row justify-end gap-2 border-t px-6 py-4'>
              <Button
                type='button'
                variant='outline'
                disabled={isSubmitting}
                onClick={() => handleOpenChange(false)}
              >
                {tCommon('cancel')}
              </Button>
              <Button type='submit' disabled={isSubmitting}>
                {isSubmitting && <Loader2 className='size-4 animate-spin' />}
                {isEdit ? t('form.submitSave') : t('form.submitCreate')}
              </Button>
            </SheetFooter>
          </form>
        </Form>
      </SheetContent>
    </Sheet>
  )
}
