import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import {
  AlertCircle,
  Camera,
  Clock,
  Loader2,
  Lock,
  RefreshCw,
  Shield,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { getDisplayNameInitials } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
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
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { AuthAlert } from '@/components/shared/auth-alert'
import {
  avatarPolicyQueryOptions,
  profileQueryOptions,
  useUpdateProfileMutation,
  useUploadAvatarMutation,
} from '../queries'
import { profileFormSchema, type ProfileFormValues } from '../schema'
import { ContentSection } from './content-section'
import { formatAvatarMaxSize, formatSettingsTimestamp } from './session-device'

const ALLOWED_AVATAR_MIME_TYPES = new Set([
  'image/jpeg',
  'image/png',
  'image/webp',
  'image/gif',
])
const DEFAULT_MAX_AVATAR_BYTES = 2 * 1024 * 1024

export function ProfileSection() {
  const { t, i18n } = useTranslation('settings')
  const { t: tCommon } = useTranslation('common')
  const { t: tValidation } = useTranslation('validation')

  const fileInputRef = useRef<HTMLInputElement>(null)
  const retriedAvatarUrlRef = useRef<string | null>(null)
  const [bannerError, setBannerError] = useState<string | null>(null)
  const [avatarError, setAvatarError] = useState<string | null>(null)
  const [avatarAction, setAvatarAction] = useState<'upload' | 'remove' | null>(
    null
  )

  const {
    data: profile,
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery(profileQueryOptions(i18n.language))

  const { data: avatarPolicy } = useQuery(avatarPolicyQueryOptions())
  const maxAvatarBytes =
    avatarPolicy?.maxAvatarSizeBytes ?? DEFAULT_MAX_AVATAR_BYTES
  const formattedMaxSize = formatAvatarMaxSize(maxAvatarBytes, i18n.language)

  const updateMutation = useUpdateProfileMutation()
  const avatarProfileMutation = useUpdateProfileMutation()
  const uploadAvatarMutation = useUploadAvatarMutation()

  const schema = useMemo(() => profileFormSchema(tValidation), [tValidation])

  const form = useForm<ProfileFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      displayName: '',
      email: '',
    },
  })

  useEffect(() => {
    if (!profile) return
    form.reset(
      {
        displayName: profile.displayName,
        email: profile.email,
      },
      { keepDirtyValues: true }
    )
  }, [profile, form])

  useEffect(() => {
    return form.subscribe({
      formState: { values: true },
      callback: () => setBannerError(null),
    })
  }, [form])

  const handleAvatarChange = async (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return

    setAvatarError(null)
    if (file.type && !ALLOWED_AVATAR_MIME_TYPES.has(file.type.toLowerCase())) {
      setAvatarError(t('profile.avatarInvalidType'))
      return
    }
    if (file.size > maxAvatarBytes) {
      setAvatarError(t('profile.avatarTooLarge', { maxSize: formattedMaxSize }))
      return
    }

    setAvatarAction('upload')
    try {
      const uploaded = await uploadAvatarMutation.mutateAsync(file)
      await avatarProfileMutation.mutateAsync({ avatarFileId: uploaded.id })
      toast.success(t('profile.avatarUpdated'))
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 401 || apiErr.code === 'AUTH_ACCOUNT_DISABLED') {
        return
      }
      setAvatarError(
        apiErr.fieldErrors.avatarFileId || formatServerErrorMessage(apiErr)
      )
    } finally {
      setAvatarAction(null)
    }
  }

  const handleRemoveAvatar = async () => {
    setAvatarError(null)
    setAvatarAction('remove')
    try {
      await avatarProfileMutation.mutateAsync({ avatarFileId: null })
      toast.success(t('profile.avatarRemoved'))
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 401 || apiErr.code === 'AUTH_ACCOUNT_DISABLED') {
        return
      }
      setAvatarError(
        apiErr.fieldErrors.avatarFileId || formatServerErrorMessage(apiErr)
      )
    } finally {
      setAvatarAction(null)
    }
  }

  const onSubmit = async (values: ProfileFormValues) => {
    setBannerError(null)
    try {
      const updated = await updateMutation.mutateAsync({
        displayName: values.displayName.trim(),
        email: values.email.trim(),
      })
      form.reset({
        displayName: updated.displayName,
        email: updated.email,
      })
      toast.success(t('profile.updated'))
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 401 || apiErr.code === 'AUTH_ACCOUNT_DISABLED') {
        return
      }
      if (apiErr.code === 'USERS_EMAIL_CONFLICT') {
        form.setError('email', { message: apiErr.message })
        return
      }
      if (apiErr.status === 422 && Object.keys(apiErr.fieldErrors).length > 0) {
        let mapped = false
        for (const [field, message] of Object.entries(apiErr.fieldErrors)) {
          if (field === 'displayName' || field === 'email') {
            form.setError(field, { message })
            mapped = true
          }
        }
        if (mapped) return
      }
      setBannerError(formatServerErrorMessage(apiErr))
    }
  }

  const isUploadingAvatar =
    avatarAction === 'upload' || uploadAvatarMutation.isPending
  const isRemovingAvatar = avatarAction === 'remove'
  const isAvatarBusy =
    isUploadingAvatar || isRemovingAvatar || avatarProfileMutation.isPending
  const isSaving = updateMutation.isPending
  const displayLabel = profile?.displayName || profile?.username || ''

  if (isLoading) {
    return (
      <ContentSection>
        <Card className='gap-0 overflow-hidden py-0'>
          <CardContent className='p-6'>
            <div className='flex flex-col gap-5 sm:flex-row sm:items-center'>
              <Skeleton className='size-20 shrink-0 rounded-full' />
              <div className='flex-1 space-y-2.5'>
                <Skeleton className='h-5 w-44' />
                <Skeleton className='h-4 w-56' />
                <div className='flex gap-2 pt-1'>
                  <Skeleton className='h-5 w-24 rounded-full' />
                  <Skeleton className='h-5 w-36 rounded-full' />
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className='gap-0 overflow-hidden py-0'>
          <CardHeader className='p-6 pb-4'>
            <Skeleton className='h-5 w-36' />
            <Skeleton className='h-4 w-72' />
          </CardHeader>
          <CardContent className='grid grid-cols-1 gap-5 px-6 pb-6 sm:grid-cols-2'>
            <Skeleton className='h-10 w-full' />
            <Skeleton className='h-10 w-full' />
            <Skeleton className='h-10 w-full sm:col-span-2' />
          </CardContent>
        </Card>
      </ContentSection>
    )
  }

  if (isError || !profile) {
    return (
      <ContentSection>
        <Alert variant='destructive'>
          <AlertCircle className='size-4' />
          <AlertTitle>{t('profile.loadFailed')}</AlertTitle>
          <AlertDescription className='flex flex-wrap items-center justify-between gap-2'>
            <span>{formatServerErrorMessage(error)}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void refetch()}
            >
              <RefreshCw className='size-3.5' />
              {tCommon('retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </ContentSection>
    )
  }

  return (
    <ContentSection>
      <Card className='gap-0 overflow-hidden py-0'>
        <CardContent className='space-y-4 p-6'>
          <div className='flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between'>
            <div className='flex min-w-0 items-center gap-4'>
              <div className='group relative shrink-0'>
                <Avatar className='size-20 border-2 border-background shadow-sm ring-1 ring-border'>
                  {profile.avatarUrl && (
                    <AvatarImage
                      data-testid='profile-avatar-image'
                      src={profile.avatarUrl}
                      alt={displayLabel}
                      onLoadingStatusChange={(status) => {
                        if (
                          status === 'error' &&
                          profile.avatarUrl &&
                          retriedAvatarUrlRef.current !== profile.avatarUrl
                        ) {
                          retriedAvatarUrlRef.current = profile.avatarUrl
                          void refetch()
                        }
                      }}
                    />
                  )}
                  <AvatarFallback className='bg-primary/10 text-lg font-semibold text-primary'>
                    {getDisplayNameInitials(displayLabel)}
                  </AvatarFallback>
                </Avatar>
                <button
                  type='button'
                  disabled={isAvatarBusy}
                  aria-label={t('profile.uploadAvatar')}
                  onClick={() => fileInputRef.current?.click()}
                  className='absolute inset-0 flex items-center justify-center rounded-full bg-foreground/55 text-background opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden disabled:pointer-events-none'
                >
                  {isUploadingAvatar ? (
                    <Loader2 className='size-5 animate-spin' />
                  ) : (
                    <Camera className='size-5' />
                  )}
                </button>
              </div>

              <div className='min-w-0 space-y-1.5'>
                <div className='flex flex-wrap items-center gap-2'>
                  <h2 className='truncate text-lg font-semibold tracking-tight'>
                    {displayLabel}
                  </h2>
                  <span className='font-mono text-xs text-muted-foreground'>
                    @{profile.username}
                  </span>
                  <Badge
                    variant='secondary'
                    className='gap-1.5 px-2 py-0.5 text-[11px] font-normal'
                  >
                    <span className='size-1.5 rounded-full bg-success' />
                    {t('profile.statusActive')}
                  </Badge>
                </div>

                <p className='truncate text-sm text-muted-foreground'>
                  {profile.email}
                </p>

                <div className='flex flex-wrap items-center gap-x-4 gap-y-1.5 pt-0.5 text-xs text-muted-foreground'>
                  <div className='flex flex-wrap items-center gap-1.5'>
                    <Shield className='size-3.5 shrink-0' />
                    {profile.roles.length > 0 ? (
                      profile.roles.map((role) => (
                        <Badge
                          key={role.id}
                          variant={role.isSystem ? 'default' : 'outline'}
                          className='px-2 py-0 text-[11px] font-medium'
                        >
                          {role.name}
                        </Badge>
                      ))
                    ) : (
                      <span>{t('profile.noRoles')}</span>
                    )}
                  </div>
                  <div className='flex items-center gap-1.5'>
                    <Clock className='size-3.5 shrink-0' />
                    <span>{t('profile.lastLoginAt')}:</span>
                    <span className='font-mono text-foreground/80'>
                      {profile.lastLoginAt
                        ? formatSettingsTimestamp(
                            profile.lastLoginAt,
                            i18n.language
                          )
                        : t('profile.neverLoggedIn')}
                    </span>
                  </div>
                </div>
              </div>
            </div>

            <div className='flex shrink-0 flex-wrap items-center gap-2 sm:flex-col sm:items-end'>
              <input
                ref={fileInputRef}
                data-testid='profile-avatar-input'
                type='file'
                accept='image/jpeg,image/png,image/webp,image/gif'
                className='hidden'
                onChange={(e) => void handleAvatarChange(e)}
              />
              <Button
                type='button'
                variant='outline'
                size='sm'
                data-testid='profile-avatar-upload'
                disabled={isAvatarBusy}
                onClick={() => fileInputRef.current?.click()}
              >
                {isUploadingAvatar ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <Camera className='size-3.5' />
                )}
                {isUploadingAvatar
                  ? t('profile.uploadingAvatar')
                  : t('profile.uploadAvatar')}
              </Button>
              {profile.avatarFileId && (
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  data-testid='profile-avatar-remove'
                  disabled={isAvatarBusy}
                  className='text-muted-foreground hover:text-destructive'
                  onClick={() => void handleRemoveAvatar()}
                >
                  {isRemovingAvatar ? (
                    <Loader2 className='size-3.5 animate-spin' />
                  ) : (
                    <Trash2 className='size-3.5' />
                  )}
                  {t('profile.removeAvatar')}
                </Button>
              )}
            </div>
          </div>

          {avatarError && <AuthAlert tone='destructive' title={avatarError} />}
        </CardContent>

        <CardFooter className='border-t bg-muted/30 px-6 py-3 [.border-t]:pt-3'>
          <p className='text-xs text-muted-foreground'>
            {t('profile.avatarHint', { maxSize: formattedMaxSize })}
          </p>
        </CardFooter>
      </Card>

      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-6 pb-4'>
          <CardTitle className='text-base'>
            {t('profile.basicInfoTitle')}
          </CardTitle>
          <CardDescription>{t('profile.basicInfoDesc')}</CardDescription>
        </CardHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((v) => void onSubmit(v))}
            noValidate
          >
            <CardContent className='space-y-5 px-6 pb-6'>
              {bannerError && (
                <AuthAlert tone='destructive' title={bannerError} />
              )}

              <div className='grid grid-cols-1 gap-5 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='displayName'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('profile.displayName')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          data-testid='profile-display-name'
                          disabled={isSaving}
                          placeholder={t('profile.displayNamePlaceholder')}
                          autoComplete='name'
                        />
                      </FormControl>
                      <FormDescription>
                        {t('profile.displayNameHint')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='email'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('profile.email')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          data-testid='profile-email'
                          type='email'
                          disabled={isSaving}
                          placeholder={t('profile.emailPlaceholder')}
                          autoComplete='email'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <div className='space-y-2 sm:max-w-sm'>
                <label
                  htmlFor='profile-username'
                  className='flex items-center gap-1.5 text-sm leading-none font-medium'
                >
                  {t('profile.username')}
                  <Lock className='size-3 text-muted-foreground' />
                </label>
                <Input
                  id='profile-username'
                  value={profile.username}
                  readOnly
                  disabled
                  className='bg-muted/40 font-mono text-muted-foreground'
                />
                <p className='text-xs text-muted-foreground'>
                  {t('profile.usernameHint')}
                </p>
              </div>
            </CardContent>

            <CardFooter className='flex flex-wrap items-center justify-between gap-3 border-t bg-muted/30 px-6 py-3.5 [.border-t]:pt-3.5'>
              <p className='text-xs text-muted-foreground'>
                {form.formState.isDirty
                  ? t('profile.unsavedChanges')
                  : t('profile.allChangesSaved')}
              </p>
              <Button
                type='submit'
                size='sm'
                data-testid='profile-submit'
                disabled={!form.formState.isDirty || isSaving}
                aria-busy={isSaving}
              >
                {isSaving && <Loader2 className='size-3.5 animate-spin' />}
                {isSaving ? t('profile.saving') : t('profile.save')}
              </Button>
            </CardFooter>
          </form>
        </Form>
      </Card>
    </ContentSection>
  )
}
