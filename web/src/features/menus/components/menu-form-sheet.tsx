import { useCallback, useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { AlertCircle, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { usePermission } from '@/lib/auth/use-permission'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { AVAILABLE_MENU_ICONS, resolveMenuIcon } from '@/lib/icons'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
import {
  menuPermissionsQueryOptions,
  useCreateMenuMutation,
  useUpdateMenuMutation,
} from '../queries'
import {
  isValidMenuParentPlacement,
  menuFormSchema,
  type MenuFormValues,
} from '../schema'
import type { MenuItem, MenuNodeType } from '../types'

const ROOT_VALUE = '__root__'
const NONE_VALUE = '__none__'

const NODE_TYPES: readonly MenuNodeType[] = ['directory', 'menu', 'button']

const MAPPABLE_FIELDS = new Set<keyof MenuFormValues>([
  'parentId',
  'type',
  'code',
  'title',
  'path',
  'icon',
  'permissionCode',
  'sortOrder',
  'visible',
  'status',
])

const BUTTON_HIDDEN_FIELDS = new Set<keyof MenuFormValues>([
  'path',
  'icon',
  'visible',
])

const DEFAULT_VALUES: MenuFormValues = {
  parentId: ROOT_VALUE,
  type: 'menu',
  code: '',
  title: '',
  path: '',
  icon: NONE_VALUE,
  permissionCode: NONE_VALUE,
  sortOrder: '10',
  visible: true,
  status: 'active',
}

type FlatParentOption = {
  id: string
  label: string
  type: MenuNodeType
}

function flattenParentOptions(
  nodes: MenuItem[],
  excludeId?: string,
  depth = 0
): FlatParentOption[] {
  const out: FlatParentOption[] = []
  for (const n of nodes) {
    if (n.id === excludeId) continue
    if (n.type !== 'button') {
      const prefix = depth > 0 ? `${'  '.repeat(depth)}└ ` : ''
      out.push({
        id: n.id,
        label: `${prefix}${n.title}`,
        type: n.type,
      })
      if (n.children.length > 0) {
        out.push(...flattenParentOptions(n.children, excludeId, depth + 1))
      }
    }
  }
  return out
}

type MenuFormSheetProps = {
  open: boolean
  mode: 'create' | 'edit'
  menu: MenuItem | null
  defaultParentId: string | null
  treeItems: MenuItem[]
  onOpenChange: (open: boolean) => void
}

export function MenuFormSheet({
  open,
  mode,
  menu,
  defaultParentId,
  treeItems,
  onOpenChange,
}: MenuFormSheetProps) {
  const { t, i18n } = useTranslation('menus')
  const { t: tCommon } = useTranslation('common')
  const { t: tValidation } = useTranslation('validation')
  const bannerKey = `${open}:${mode}:${menu?.id ?? ''}:${defaultParentId ?? ''}`
  const [bannerState, setBannerState] = useState<{
    key: string
    message: string
  } | null>(null)
  const bannerError =
    bannerState?.key === bannerKey ? bannerState.message : null

  const isEdit = mode === 'edit' && menu !== null
  const canReadPermissions = usePermission('roles:read')

  const {
    data: permsData,
    isLoading: isPermsLoading,
    isError: isPermsError,
    refetch: refetchPerms,
  } = useQuery({
    ...menuPermissionsQueryOptions(i18n.language),
    enabled: open && canReadPermissions,
  })
  const permissionOptions = useMemo(() => {
    const groups = permsData?.groups ?? []
    return groups.flatMap((g) => g.permissions)
  }, [permsData])

  const excludeMenuId = isEdit ? menu.id : undefined
  const parentOptions = useMemo(
    () => flattenParentOptions(treeItems, excludeMenuId),
    [treeItems, excludeMenuId]
  )
  const parentTypeById = useMemo(
    () => new Map(parentOptions.map((opt) => [opt.id, opt.type])),
    [parentOptions]
  )

  const resolveParentType = useCallback(
    (parentId: string) => {
      if (parentId === ROOT_VALUE) return null
      return parentTypeById.get(parentId)
    },
    [parentTypeById]
  )

  const schema = useMemo(
    () => menuFormSchema(tValidation, resolveParentType),
    [tValidation, resolveParentType]
  )

  const form = useForm<MenuFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_VALUES,
  })

  const watchedType = useWatch({ control: form.control, name: 'type' })

  useEffect(() => {
    if (!open) {
      form.reset(DEFAULT_VALUES)
      return
    }
    if (isEdit) {
      form.reset({
        parentId: menu.parentId ?? ROOT_VALUE,
        type: menu.type,
        code: menu.code,
        title: menu.title,
        path: menu.path,
        icon: menu.icon || NONE_VALUE,
        permissionCode: menu.permissionCode || NONE_VALUE,
        sortOrder: String(menu.sortOrder),
        visible: menu.visible,
        status: menu.status,
      })
    } else {
      form.reset({
        ...DEFAULT_VALUES,
        parentId: defaultParentId ?? ROOT_VALUE,
      })
    }
  }, [open, isEdit, menu, defaultParentId, form])

  const createMutation = useCreateMenuMutation()
  const updateMutation = useUpdateMenuMutation()
  const isSubmitting = createMutation.isPending || updateMutation.isPending

  const clearBannerError = () => {
    if (bannerError) setBannerState(null)
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && isSubmitting) return
    if (!nextOpen) setBannerState(null)
    onOpenChange(nextOpen)
  }

  const canSelectNodeType = (candidateType: MenuNodeType): boolean => {
    if (!isEdit || menu.children.length === 0) return true
    return menu.children.every((child) =>
      isValidMenuParentPlacement(candidateType, child.type)
    )
  }

  const onSubmit = async (values: MenuFormValues) => {
    setBannerState(null)
    const isButton = values.type === 'button'
    const parentId =
      values.parentId === ROOT_VALUE ? null : values.parentId.trim()
    const path = isButton ? '' : values.path.trim()
    const icon =
      isButton || values.icon === NONE_VALUE ? '' : values.icon.trim()
    const permissionCode =
      values.permissionCode === NONE_VALUE ? '' : values.permissionCode.trim()
    const sortOrder = Number(values.sortOrder)
    const visible = isButton ? false : values.visible

    try {
      if (isEdit) {
        await updateMutation.mutateAsync({
          id: menu.id,
          payload: {
            parentId,
            type: menu.isSystem ? undefined : values.type,
            title: values.title.trim(),
            path,
            icon,
            permissionCode,
            sortOrder,
            visible,
            status: values.status,
          },
        })
        toast.success(t('toast.updated'))
      } else {
        await createMutation.mutateAsync({
          parentId,
          type: values.type,
          code: values.code.trim(),
          title: values.title.trim(),
          path,
          icon,
          permissionCode,
          sortOrder,
          visible,
          status: values.status,
        })
        toast.success(t('toast.created'))
      }
      onOpenChange(false)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.code === 'MENUS_CODE_CONFLICT') {
        form.setError('code', { message: apiErr.message })
        return
      }
      if (
        apiErr.status === 422 &&
        apiErr.fieldErrors &&
        Object.keys(apiErr.fieldErrors).length > 0
      ) {
        let allMapped = true
        for (const [field, message] of Object.entries(apiErr.fieldErrors)) {
          const typedField = field as keyof MenuFormValues
          const isMounted =
            MAPPABLE_FIELDS.has(typedField) &&
            (!isButton || !BUTTON_HIDDEN_FIELDS.has(typedField))
          if (isMounted) {
            form.setError(typedField, { message })
          } else {
            allMapped = false
          }
        }
        if (allMapped) return
      }
      setBannerState({
        key: bannerKey,
        message: formatServerErrorMessage(err),
      })
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
            onChange={clearBannerError}
            className='flex min-h-0 flex-1 flex-col'
          >
            <ScrollArea className='min-h-0 flex-1'>
              <div className='space-y-5 px-6 py-5'>
                {bannerError && (
                  <AuthAlert tone='destructive' title={bannerError} />
                )}

                <FormField
                  control={form.control}
                  name='type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.type')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={(nextValue) => {
                          clearBannerError()
                          field.onChange(nextValue)
                        }}
                        disabled={isSubmitting || (isEdit && menu.isSystem)}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {NODE_TYPES.map((nodeType) => (
                            <SelectItem
                              key={nodeType}
                              value={nodeType}
                              disabled={!canSelectNodeType(nodeType)}
                            >
                              {t(`types.${nodeType}`)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='parentId'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.parent')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={(nextValue) => {
                          clearBannerError()
                          field.onChange(nextValue)
                        }}
                        disabled={isSubmitting}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem
                            value={ROOT_VALUE}
                            disabled={
                              !isValidMenuParentPlacement(null, watchedType)
                            }
                          >
                            {t('form.rootParent')}
                          </SelectItem>
                          {parentOptions.map((opt) => (
                            <SelectItem
                              key={opt.id}
                              value={opt.id}
                              disabled={
                                !isValidMenuParentPlacement(
                                  opt.type,
                                  watchedType
                                )
                              }
                            >
                              {opt.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='code'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.code')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          readOnly={isEdit}
                          disabled={isEdit || isSubmitting}
                          placeholder={t('form.codePlaceholder')}
                          className='font-mono text-sm'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='title'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.title')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          disabled={isSubmitting}
                          placeholder={t('form.titlePlaceholder')}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {watchedType !== 'button' && (
                  <>
                    <FormField
                      control={form.control}
                      name='path'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('form.path')}</FormLabel>
                          <FormControl>
                            <Input
                              {...field}
                              disabled={isSubmitting}
                              placeholder={t('form.pathPlaceholder')}
                              className='font-mono text-sm'
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='icon'
                      render={({ field }) => {
                        const hasCustomIcon =
                          field.value !== '' &&
                          field.value !== NONE_VALUE &&
                          !AVAILABLE_MENU_ICONS.includes(field.value)
                        return (
                          <FormItem>
                            <FormLabel>{t('form.icon')}</FormLabel>
                            <Select
                              value={field.value}
                              onValueChange={(nextValue) => {
                                clearBannerError()
                                field.onChange(nextValue)
                              }}
                              disabled={isSubmitting}
                            >
                              <FormControl>
                                <SelectTrigger className='w-full'>
                                  <SelectValue
                                    placeholder={t('form.iconPlaceholder')}
                                  />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent>
                                <SelectItem value={NONE_VALUE}>
                                  {t('form.noIcon')}
                                </SelectItem>
                                {hasCustomIcon && (
                                  <SelectItem value={field.value}>
                                    {field.value}
                                  </SelectItem>
                                )}
                                {AVAILABLE_MENU_ICONS.map((iconName) => {
                                  const IconComp = resolveMenuIcon(iconName)
                                  return (
                                    <SelectItem key={iconName} value={iconName}>
                                      <span className='flex items-center gap-2'>
                                        {IconComp && (
                                          <IconComp className='size-4' />
                                        )}
                                        <span>{iconName}</span>
                                      </span>
                                    </SelectItem>
                                  )
                                })}
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )
                      }}
                    />
                  </>
                )}

                <FormField
                  control={form.control}
                  name='permissionCode'
                  render={({ field }) => {
                    const hasFallbackPerm =
                      field.value !== '' &&
                      field.value !== NONE_VALUE &&
                      !permissionOptions.some((p) => p.code === field.value)
                    return (
                      <FormItem>
                        <FormLabel>{t('form.permissionCode')}</FormLabel>
                        <Select
                          value={field.value}
                          onValueChange={(nextValue) => {
                            clearBannerError()
                            field.onChange(nextValue)
                          }}
                          disabled={isSubmitting}
                        >
                          <FormControl>
                            <SelectTrigger className='w-full'>
                              <SelectValue
                                placeholder={t('form.permissionPlaceholder')}
                              />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent>
                            <SelectItem value={NONE_VALUE}>
                              {t('form.noPermission')}
                            </SelectItem>
                            {hasFallbackPerm && (
                              <SelectItem value={field.value}>
                                <span className='font-mono text-xs'>
                                  {field.value}
                                </span>
                              </SelectItem>
                            )}
                            {permissionOptions.map((perm) => (
                              <SelectItem key={perm.code} value={perm.code}>
                                <span className='font-mono text-xs'>
                                  {perm.code}
                                </span>{' '}
                                <span className='text-muted-foreground'>
                                  ({perm.name})
                                </span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        {canReadPermissions && isPermsLoading && (
                          <FormDescription>
                            {t('form.permissionsLoading')}
                          </FormDescription>
                        )}
                        {canReadPermissions && isPermsError && (
                          <div className='flex items-center justify-between gap-2 text-xs text-destructive'>
                            <span className='flex items-center gap-1'>
                              <AlertCircle className='size-3.5 shrink-0' />
                              {t('form.permissionsLoadFailed')}
                            </span>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              className='h-6 px-2 text-xs text-foreground'
                              onClick={() => void refetchPerms()}
                            >
                              {tCommon('retry')}
                            </Button>
                          </div>
                        )}
                        {!canReadPermissions && (
                          <FormDescription>
                            {t('form.permissionsRestrictedHint')}
                          </FormDescription>
                        )}
                        <FormMessage />
                      </FormItem>
                    )
                  }}
                />

                <FormField
                  control={form.control}
                  name='sortOrder'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.sortOrder')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={99999}
                          {...field}
                          disabled={isSubmitting}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {watchedType !== 'button' && (
                  <FormField
                    control={form.control}
                    name='visible'
                    render={({ field }) => (
                      <FormItem>
                        <div className='flex items-center justify-between rounded-lg border p-3'>
                          <div className='space-y-0.5'>
                            <FormLabel>{t('form.visible')}</FormLabel>
                            <FormDescription>
                              {t('form.visibleHint')}
                            </FormDescription>
                          </div>
                          <FormControl>
                            <Switch
                              checked={field.value}
                              onCheckedChange={(checked) => {
                                clearBannerError()
                                field.onChange(checked)
                              }}
                              disabled={isSubmitting}
                            />
                          </FormControl>
                        </div>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='status'
                  render={({ field }) => (
                    <FormItem>
                      <div className='flex items-center justify-between rounded-lg border p-3'>
                        <div className='space-y-0.5'>
                          <FormLabel>{t('form.status')}</FormLabel>
                          <FormDescription>
                            {t('form.statusHint')}
                          </FormDescription>
                        </div>
                        <FormControl>
                          <Switch
                            checked={field.value === 'active'}
                            onCheckedChange={(checked) => {
                              clearBannerError()
                              field.onChange(checked ? 'active' : 'disabled')
                            }}
                            disabled={isSubmitting}
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
                {isSubmitting && (
                  <Loader2 className='me-2 size-4 animate-spin' />
                )}
                {isEdit ? t('form.submitSave') : t('form.submitCreate')}
              </Button>
            </SheetFooter>
          </form>
        </Form>
      </SheetContent>
    </Sheet>
  )
}
