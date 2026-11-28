import { useEffect, useId, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { AlertCircle, Loader2, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'
import { useRetryCountdown } from '@/hooks/use-retry-countdown'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
  useFormField,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { RateLimitAlert } from '@/components/shared/auth-alert'
import {
  permissionsCatalogQueryOptions,
  roleMenuTreeQueryOptions,
  useCreateRoleMutation,
  useUpdateRoleMutation,
} from '../queries'
import {
  createRoleFormSchema,
  updateRoleFormSchema,
  type RoleFormValues,
} from '../schema'
import type { RoleItem } from '../types'
import { RolePermissionTree } from './role-permission-tree'

export type RoleSheetMode = 'create' | 'edit' | 'view'

type RoleFormSheetProps = {
  open: boolean
  mode: RoleSheetMode
  role: RoleItem | null
  canUpdate?: boolean
  onOpenChange: (open: boolean) => void
}

const EMPTY_VALUES: RoleFormValues = {
  code: '',
  name: '',
  description: '',
  permissions: [],
}

export function RoleFormSheet({
  open,
  mode,
  role,
  canUpdate = true,
  onOpenChange,
}: RoleFormSheetProps) {
  const { t, i18n } = useTranslation('roles')
  const { t: tCommon } = useTranslation('common')
  const { t: tValidation } = useTranslation('validation')
  const [bannerError, setBannerError] = useState<string | null>(null)
  const sessionKey = `${open ? '1' : '0'}:${mode}:${role?.id ?? ''}`
  const [prevSessionKey, setPrevSessionKey] = useState(sessionKey)

  if (sessionKey !== prevSessionKey) {
    setPrevSessionKey(sessionKey)
    setBannerError(null)
  }

  const isReadOnly = mode === 'view' || Boolean(role?.isSystem)
  const isEdit = mode === 'edit' && role !== null && !role.isSystem
  // Viewing a custom role through the read-only entry is not a permission problem
  // when the user holds roles:update, so only show the read-only contact hint when
  // update permission is absent.
  const isCustomView =
    mode === 'view' && role !== null && !role.isSystem && canUpdate

  const schema = useMemo(
    () =>
      isEdit
        ? updateRoleFormSchema(tValidation)
        : createRoleFormSchema(tValidation),
    [isEdit, tValidation]
  )

  const form = useForm<RoleFormValues>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY_VALUES,
  })

  const {
    data: catalog,
    isLoading: isCatalogLoading,
    isError: isCatalogError,
    refetch: refetchCatalog,
  } = useQuery({
    ...permissionsCatalogQueryOptions(i18n.language),
    enabled: open,
  })

  const {
    data: menuTreeData,
    isLoading: isMenuTreeLoading,
    isError: isMenuTreeError,
  } = useQuery({
    ...roleMenuTreeQueryOptions(i18n.language),
    enabled: open,
  })

  const createMutation = useCreateRoleMutation()
  const updateMutation = useUpdateRoleMutation()
  const cooldown = useRetryCountdown()
  const isSubmitting = createMutation.isPending || updateMutation.isPending

  useEffect(() => {
    if (!open) {
      form.reset(EMPTY_VALUES)
      return
    }
    if (role && (mode === 'edit' || mode === 'view')) {
      form.reset({
        code: role.code,
        name: role.name,
        description: role.description,
        permissions: [...role.permissions],
      })
    } else {
      form.reset(EMPTY_VALUES)
    }
  }, [open, mode, role, form])

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      if (isSubmitting) return
      setBannerError(null)
      form.reset(EMPTY_VALUES)
    }
    onOpenChange(nextOpen)
  }

  const watchedPermissions = useWatch({
    control: form.control,
    name: 'permissions',
  })
  const selectedPermissions = useMemo(
    () => watchedPermissions ?? [],
    [watchedPermissions]
  )

  const allPermissionCodes = useMemo(() => {
    if (!catalog) return []
    return Array.from(
      new Set(catalog.groups.flatMap((g) => g.permissions.map((p) => p.code)))
    ).sort()
  }, [catalog])
  const catalogReady = !isCatalogLoading && !isCatalogError && Boolean(catalog)

  const updatePermissionsField = (next: string[]) => {
    setBannerError(null)
    form.setValue('permissions', next, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  const handleSubmit = form.handleSubmit(async (values) => {
    if (isReadOnly) return
    setBannerError(null)
    try {
      if (isEdit && role) {
        await updateMutation.mutateAsync({
          id: role.id,
          payload: {
            name: values.name.trim(),
            description: values.description.trim(),
            permissions: values.permissions,
          },
        })
        toast.success(t('toast.updated'))
      } else {
        await createMutation.mutateAsync({
          code: values.code.trim().toLowerCase(),
          name: values.name.trim(),
          description: values.description.trim(),
          permissions: values.permissions,
        })
        toast.success(t('toast.created'))
      }
      form.reset(EMPTY_VALUES)
      onOpenChange(false)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 429) {
        cooldown.start(apiErr.retryAfter)
        return
      }
      const entries = Object.entries(apiErr.fieldErrors)
      if (apiErr.status === 422 && entries.length > 0) {
        let allMapped = true
        for (const [field, message] of entries) {
          if (
            field === 'code' ||
            field === 'name' ||
            field === 'description' ||
            field === 'permissions'
          ) {
            form.setError(field, { message })
          } else {
            allMapped = false
          }
        }
        if (!allMapped) {
          setBannerError(formatServerErrorMessage(err))
        }
      } else {
        setBannerError(formatServerErrorMessage(err))
      }
    }
  })

  const sheetTitle =
    mode === 'create'
      ? t('sheet.createTitle')
      : isReadOnly
        ? t('sheet.viewTitle')
        : t('sheet.editTitle')

  const sheetDescription =
    mode === 'create'
      ? t('sheet.createDesc')
      : role?.isSystem
        ? t('sheet.viewSystemDesc')
        : isCustomView
          ? t('sheet.viewCustomDesc')
          : isReadOnly
            ? t('sheet.viewReadOnlyDesc')
            : t('sheet.editDesc')

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent
        side='right'
        className='flex w-full flex-col gap-0 p-0 sm:max-w-xl'
      >
        <SheetHeader className='border-b px-6 py-4'>
          <div className='flex items-center gap-2'>
            <SheetTitle>{sheetTitle}</SheetTitle>
            {role?.isSystem && (
              <Badge variant='secondary' className='gap-1 font-normal'>
                <ShieldCheck className='size-3' />
                {t('badges.system')}
              </Badge>
            )}
          </div>
          <SheetDescription>{sheetDescription}</SheetDescription>
        </SheetHeader>

        <Form {...form}>
          <form
            id='role-form'
            onSubmit={(e) => void handleSubmit(e)}
            onChange={() => {
              if (bannerError) setBannerError(null)
            }}
            className='flex min-h-0 flex-1 flex-col'
          >
            <ScrollArea className='min-h-0 flex-1'>
              <div className='space-y-5 px-6 py-5'>
                {cooldown.isCoolingDown && (
                  <RateLimitAlert seconds={cooldown.remaining} />
                )}
                {!cooldown.isCoolingDown && bannerError && (
                  <Alert variant='destructive'>
                    <AlertCircle className='size-4' />
                    <AlertDescription>{bannerError}</AlertDescription>
                  </Alert>
                )}

                <FormField
                  control={form.control}
                  name='code'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('sheet.codeLabel')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('sheet.codePlaceholder')}
                          disabled={mode !== 'create' || isSubmitting}
                          className='font-mono text-sm'
                        />
                      </FormControl>
                      {mode === 'create' && (
                        <FormDescription className='text-xs'>
                          {t('sheet.codeHint')}
                        </FormDescription>
                      )}
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('sheet.nameLabel')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('sheet.namePlaceholder')}
                          disabled={isReadOnly || isSubmitting}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='description'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('sheet.descriptionLabel')}</FormLabel>
                      <FormControl>
                        <Textarea
                          {...field}
                          rows={2}
                          placeholder={t('sheet.descriptionPlaceholder')}
                          disabled={isReadOnly || isSubmitting}
                          className='resize-none'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <Separator />

                <FormField
                  control={form.control}
                  name='permissions'
                  render={() => (
                    <FormItem className='space-y-3'>
                      <PermissionsFieldBody
                        label={t('sheet.permissionsLabel')}
                        selectedSummary={t('sheet.selectedSummary', {
                          selected: selectedPermissions.length,
                          total: allPermissionCodes.length,
                        })}
                        showSummary={catalogReady}
                        showBulkActions={
                          !isReadOnly && allPermissionCodes.length > 0
                        }
                        selectAllLabel={t('sheet.selectAll')}
                        clearAllLabel={t('sheet.clearAll')}
                        loadFailedLabel={t('sheet.permissionsLoadFailed')}
                        retryLabel={tCommon('retry')}
                        isSubmitting={isSubmitting}
                        isReadOnly={isReadOnly}
                        isCatalogError={isCatalogError}
                        isLoadingTree={
                          isCatalogLoading ||
                          (isMenuTreeLoading && !isMenuTreeError)
                        }
                        menuTree={
                          isMenuTreeError ? [] : (menuTreeData?.items ?? [])
                        }
                        groups={catalog?.groups ?? []}
                        selectedPermissions={selectedPermissions}
                        onSelectAll={() =>
                          updatePermissionsField(allPermissionCodes)
                        }
                        onClearAll={() => updatePermissionsField([])}
                        onRetryCatalog={() => void refetchCatalog()}
                        onChangePermissions={updatePermissionsField}
                      />
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </ScrollArea>

            <SheetFooter className='flex-row justify-end gap-2 border-t px-6 py-4'>
              {isReadOnly ? (
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => handleOpenChange(false)}
                >
                  {t('sheet.close')}
                </Button>
              ) : (
                <>
                  <Button
                    type='button'
                    variant='outline'
                    disabled={isSubmitting}
                    onClick={() => handleOpenChange(false)}
                  >
                    {tCommon('cancel')}
                  </Button>
                  <Button
                    type='submit'
                    disabled={isSubmitting || cooldown.isCoolingDown}
                  >
                    {isSubmitting && (
                      <Loader2 className='size-4 animate-spin' />
                    )}
                    {isEdit ? t('sheet.saveEdit') : t('sheet.saveCreate')}
                  </Button>
                </>
              )}
            </SheetFooter>
          </form>
        </Form>
      </SheetContent>
    </Sheet>
  )
}

type PermissionsFieldBodyProps = {
  label: string
  selectedSummary: string
  showSummary: boolean
  showBulkActions: boolean
  selectAllLabel: string
  clearAllLabel: string
  loadFailedLabel: string
  retryLabel: string
  isSubmitting: boolean
  isReadOnly: boolean
  isCatalogError: boolean
  isLoadingTree: boolean
  menuTree: Parameters<typeof RolePermissionTree>[0]['menuTree']
  groups: Parameters<typeof RolePermissionTree>[0]['groups']
  selectedPermissions: string[]
  onSelectAll: () => void
  onClearAll: () => void
  onRetryCatalog: () => void
  onChangePermissions: (next: string[]) => void
}

function PermissionsFieldBody({
  label,
  selectedSummary,
  showSummary,
  showBulkActions,
  selectAllLabel,
  clearAllLabel,
  loadFailedLabel,
  retryLabel,
  isSubmitting,
  isReadOnly,
  isCatalogError,
  isLoadingTree,
  menuTree,
  groups,
  selectedPermissions,
  onSelectAll,
  onClearAll,
  onRetryCatalog,
  onChangePermissions,
}: PermissionsFieldBodyProps) {
  const labelId = useId()
  const { error, formMessageId } = useFormField()

  return (
    <>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <span
            id={labelId}
            className={cn(
              'text-sm leading-none font-medium',
              error && 'text-destructive'
            )}
          >
            {label}
          </span>
          {showSummary && (
            <Badge variant='outline' className='font-mono text-xs tabular-nums'>
              {selectedSummary}
            </Badge>
          )}
        </div>

        {showBulkActions && (
          <div className='flex items-center gap-1.5'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='h-7 px-2 text-xs'
              disabled={isSubmitting}
              onClick={onSelectAll}
            >
              {selectAllLabel}
            </Button>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='h-7 px-2 text-xs'
              disabled={isSubmitting}
              onClick={onClearAll}
            >
              {clearAllLabel}
            </Button>
          </div>
        )}
      </div>

      {isCatalogError ? (
        <Alert variant='destructive'>
          <AlertCircle className='size-4' />
          <AlertDescription className='flex items-center justify-between gap-2'>
            <span>{loadFailedLabel}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={onRetryCatalog}
            >
              {retryLabel}
            </Button>
          </AlertDescription>
        </Alert>
      ) : isLoadingTree ? (
        <div className='space-y-3'>
          {Array.from({ length: 3 }).map((_, idx) => (
            <Skeleton
              key={`perm-skel-${idx}`}
              className='h-24 w-full rounded-lg'
            />
          ))}
        </div>
      ) : (
        <RolePermissionTree
          ariaLabelledBy={labelId}
          ariaDescribedBy={error ? formMessageId : undefined}
          ariaInvalid={Boolean(error)}
          menuTree={menuTree}
          groups={groups}
          selectedPermissions={selectedPermissions}
          disabled={isReadOnly || isSubmitting}
          onChange={onChangePermissions}
        />
      )}
    </>
  )
}
