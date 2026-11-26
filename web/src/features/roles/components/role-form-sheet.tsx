import { useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { AlertCircle, Loader2, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import {
  permissionsCatalogQueryOptions,
  useCreateRoleMutation,
  useUpdateRoleMutation,
} from '../queries'
import {
  createRoleFormSchema,
  updateRoleFormSchema,
  type RoleFormValues,
} from '../schema'
import type { RoleItem } from '../types'

export type RoleSheetMode = 'create' | 'edit' | 'view'

type RoleFormSheetProps = {
  open: boolean
  mode: RoleSheetMode
  role: RoleItem | null
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
  // Viewing a custom role through the read-only entry is not a permission problem,
  // so it must not reuse the "contact an administrator" copy.
  const isCustomView = mode === 'view' && role !== null && !role.isSystem

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

  const createMutation = useCreateRoleMutation()
  const updateMutation = useUpdateRoleMutation()
  const isSubmitting = createMutation.isPending || updateMutation.isPending

  useEffect(() => {
    if (!open) {
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
  const selectedSet = useMemo(
    () => new Set(selectedPermissions),
    [selectedPermissions]
  )

  const allPermissionCodes = useMemo(() => {
    if (!catalog) return []
    return catalog.groups.flatMap((g) => g.permissions.map((p) => p.code))
  }, [catalog])

  const updatePermissionsField = (next: string[]) => {
    setBannerError(null)
    form.setValue('permissions', next, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  const togglePermission = (code: string, checked: boolean) => {
    if (isReadOnly) return
    const next = new Set(selectedPermissions)
    if (checked) {
      next.add(code)
    } else {
      next.delete(code)
    }
    updatePermissionsField(Array.from(next))
  }

  const toggleGroup = (codes: string[], checked: boolean) => {
    if (isReadOnly) return
    const next = new Set(selectedPermissions)
    for (const code of codes) {
      if (checked) {
        next.add(code)
      } else {
        next.delete(code)
      }
    }
    updatePermissionsField(Array.from(next))
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
      const entries = Object.entries(apiErr.fieldErrors)
      if (apiErr.status === 422 && entries.length > 0) {
        let mapped = false
        for (const [field, message] of entries) {
          if (
            field === 'code' ||
            field === 'name' ||
            field === 'description' ||
            field === 'permissions'
          ) {
            form.setError(field, { message })
            mapped = true
          }
        }
        if (!mapped) {
          setBannerError(apiErr.message)
        }
      } else {
        setBannerError(apiErr.message)
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
                {bannerError && (
                  <Alert variant='destructive' role='alert'>
                    <AlertCircle className='size-4' />
                    <AlertDescription>{bannerError}</AlertDescription>
                  </Alert>
                )}

                <div className='grid gap-4 sm:grid-cols-2'>
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
                </div>

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
                      <div className='flex flex-wrap items-center justify-between gap-2'>
                        <div className='flex items-center gap-2'>
                          <FormLabel className='text-sm font-medium'>
                            {t('sheet.permissionsLabel')}
                          </FormLabel>
                          <Badge
                            variant='outline'
                            className='font-mono text-xs tabular-nums'
                          >
                            {t('sheet.selectedSummary', {
                              selected: selectedPermissions.length,
                              total: allPermissionCodes.length,
                            })}
                          </Badge>
                        </div>

                        {!isReadOnly && allPermissionCodes.length > 0 && (
                          <div className='flex items-center gap-1.5'>
                            <Button
                              type='button'
                              variant='ghost'
                              size='sm'
                              className='h-7 px-2 text-xs'
                              disabled={isSubmitting}
                              onClick={() =>
                                updatePermissionsField(allPermissionCodes)
                              }
                            >
                              {t('sheet.selectAll')}
                            </Button>
                            <Button
                              type='button'
                              variant='ghost'
                              size='sm'
                              className='h-7 px-2 text-xs'
                              disabled={isSubmitting}
                              onClick={() => updatePermissionsField([])}
                            >
                              {t('sheet.clearAll')}
                            </Button>
                          </div>
                        )}
                      </div>

                      {isCatalogLoading ? (
                        <div className='space-y-3'>
                          {Array.from({ length: 3 }).map((_, idx) => (
                            <Skeleton
                              key={`perm-skel-${idx}`}
                              className='h-24 w-full rounded-lg'
                            />
                          ))}
                        </div>
                      ) : isCatalogError ? (
                        <Alert variant='destructive'>
                          <AlertCircle className='size-4' />
                          <AlertDescription className='flex items-center justify-between gap-2'>
                            <span>{t('sheet.permissionsLoadFailed')}</span>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() => void refetchCatalog()}
                            >
                              {tCommon('retry')}
                            </Button>
                          </AlertDescription>
                        </Alert>
                      ) : (
                        <div className='space-y-3'>
                          {catalog?.groups.map((group) => {
                            const groupCodes = group.permissions.map(
                              (p) => p.code
                            )
                            const selectedCount = groupCodes.filter((c) =>
                              selectedSet.has(c)
                            ).length
                            const allSelected =
                              groupCodes.length > 0 &&
                              selectedCount === groupCodes.length
                            const someSelected =
                              selectedCount > 0 && !allSelected
                            const groupChecked: boolean | 'indeterminate' =
                              allSelected
                                ? true
                                : someSelected
                                  ? 'indeterminate'
                                  : false

                            return (
                              <div
                                key={group.module}
                                className='rounded-lg border bg-card'
                              >
                                <div className='flex items-center justify-between border-b bg-muted/35 px-3.5 py-2.5'>
                                  <label className='flex cursor-pointer items-center gap-2.5 text-sm font-medium'>
                                    <Checkbox
                                      checked={groupChecked}
                                      disabled={isReadOnly || isSubmitting}
                                      aria-label={group.label}
                                      className='data-[state=indeterminate]:border-primary data-[state=indeterminate]:bg-primary/20 data-[state=indeterminate]:text-primary'
                                      onCheckedChange={(checked) =>
                                        toggleGroup(
                                          groupCodes,
                                          checked === true
                                        )
                                      }
                                    />
                                    <span>{group.label}</span>
                                  </label>
                                  <span className='font-mono text-xs text-muted-foreground tabular-nums'>
                                    {selectedCount}/{groupCodes.length}
                                  </span>
                                </div>

                                <div className='grid gap-1 p-2 sm:grid-cols-2'>
                                  {group.permissions.map((perm) => {
                                    const checked = selectedSet.has(perm.code)
                                    return (
                                      <label
                                        key={perm.code}
                                        className='flex cursor-pointer items-start gap-2.5 rounded-md px-2 py-2 transition-colors hover:bg-muted/50'
                                      >
                                        <Checkbox
                                          checked={checked}
                                          disabled={isReadOnly || isSubmitting}
                                          className='mt-0.5'
                                          onCheckedChange={(val) =>
                                            togglePermission(
                                              perm.code,
                                              val === true
                                            )
                                          }
                                        />
                                        <div className='min-w-0 flex-1'>
                                          <div className='text-sm leading-tight font-medium'>
                                            {perm.name}
                                          </div>
                                          {perm.description && (
                                            <p className='mt-0.5 text-xs leading-snug text-muted-foreground'>
                                              {perm.description}
                                            </p>
                                          )}
                                          <span className='mt-1 inline-block font-mono text-[11px] text-muted-foreground/80'>
                                            {perm.code}
                                          </span>
                                        </div>
                                      </label>
                                    )
                                  })}
                                </div>
                              </div>
                            )
                          })}
                        </div>
                      )}
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
                  <Button type='submit' disabled={isSubmitting}>
                    {isSubmitting && (
                      <Loader2 className='me-2 size-4 animate-spin' />
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
