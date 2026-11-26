import { useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
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
import { menuFormSchema, type MenuFormValues } from '../schema'
import type { MenuItem } from '../types'

const ROOT_VALUE = '__root__'
const NONE_VALUE = '__none__'

const DEFAULT_VALUES: MenuFormValues = {
  parentId: ROOT_VALUE,
  type: 'menu',
  code: '',
  title: '',
  path: '',
  icon: NONE_VALUE,
  permissionCode: NONE_VALUE,
  sortOrder: 10,
  visible: true,
  status: 'active',
}

type FlatParentOption = {
  id: string
  label: string
  type: MenuItem['type']
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
  const [bannerError, setBannerError] = useState<string | null>(null)

  const isEdit = mode === 'edit' && menu !== null

  const { data: permsData } = useQuery({
    ...menuPermissionsQueryOptions(i18n.language),
    enabled: open,
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

  const schema = useMemo(() => menuFormSchema(tValidation), [tValidation])

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
        sortOrder: menu.sortOrder,
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

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && isSubmitting) return
    if (!nextOpen) setBannerError(null)
    onOpenChange(nextOpen)
  }

  const onSubmit = async (values: MenuFormValues) => {
    setBannerError(null)
    const parentId =
      values.parentId === ROOT_VALUE ? null : values.parentId.trim()
    const icon = values.icon === NONE_VALUE ? '' : values.icon.trim()
    const permissionCode =
      values.permissionCode === NONE_VALUE ? '' : values.permissionCode.trim()

    try {
      if (isEdit) {
        await updateMutation.mutateAsync({
          id: menu.id,
          payload: {
            parentId,
            type: menu.isSystem ? undefined : values.type,
            title: values.title.trim(),
            path: values.path.trim(),
            icon,
            permissionCode,
            sortOrder: values.sortOrder,
            visible: values.visible,
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
          path: values.path.trim(),
          icon,
          permissionCode,
          sortOrder: values.sortOrder,
          visible: values.visible,
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
        let mapped = false
        for (const [field, message] of Object.entries(apiErr.fieldErrors)) {
          if (
            field === 'parentId' ||
            field === 'type' ||
            field === 'code' ||
            field === 'title' ||
            field === 'path' ||
            field === 'icon' ||
            field === 'permissionCode' ||
            field === 'sortOrder' ||
            field === 'status'
          ) {
            form.setError(field, { message })
            mapped = true
          }
        }
        if (mapped) return
      }
      setBannerError(apiErr.message)
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
                  name='type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.type')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={field.onChange}
                        disabled={isSubmitting || (isEdit && menu.isSystem)}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value='directory'>
                            {t('types.directory')}
                          </SelectItem>
                          <SelectItem value='menu'>
                            {t('types.menu')}
                          </SelectItem>
                          <SelectItem value='button'>
                            {t('types.button')}
                          </SelectItem>
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
                        onValueChange={field.onChange}
                        disabled={isSubmitting}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value={ROOT_VALUE}>
                            {t('form.rootParent')}
                          </SelectItem>
                          {parentOptions.map((opt) => (
                            <SelectItem key={opt.id} value={opt.id}>
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
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('form.icon')}</FormLabel>
                          <Select
                            value={field.value}
                            onValueChange={field.onChange}
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
                      )}
                    />
                  </>
                )}

                <FormField
                  control={form.control}
                  name='permissionCode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('form.permissionCode')}</FormLabel>
                      <Select
                        value={field.value}
                        onValueChange={field.onChange}
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
                      <FormMessage />
                    </FormItem>
                  )}
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
                          value={field.value}
                          onChange={(e) =>
                            field.onChange(Number(e.target.value) || 0)
                          }
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
                      <FormItem className='flex items-center justify-between rounded-lg border p-3'>
                        <div className='space-y-0.5'>
                          <FormLabel>{t('form.visible')}</FormLabel>
                          <FormDescription>
                            {t('form.visibleHint')}
                          </FormDescription>
                        </div>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                            disabled={isSubmitting}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='status'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between rounded-lg border p-3'>
                      <div className='space-y-0.5'>
                        <FormLabel>{t('form.status')}</FormLabel>
                        <FormDescription>
                          {t('form.statusHint')}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value === 'active'}
                          onCheckedChange={(checked) =>
                            field.onChange(checked ? 'active' : 'disabled')
                          }
                          disabled={isSubmitting}
                        />
                      </FormControl>
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
