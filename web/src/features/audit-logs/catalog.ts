/**
 * Single source of truth for audit action/resource labels and filters.
 *
 * Keep this list aligned with the server-side audit action constants. A missing
 * entry degrades the table to raw codes and makes the dimension impossible to
 * filter from the UI, so new server actions must be added here together with
 * the matching i18n keys.
 */
export type AuditCatalogOption = {
  value: string
  labelKey: string
}

export const AUDIT_ACTION_OPTIONS: readonly AuditCatalogOption[] = [
  { value: 'setup.init', labelKey: 'actions.setupInit' },
  { value: 'auth.login', labelKey: 'actions.authLogin' },
  { value: 'auth.logout', labelKey: 'actions.authLogout' },
  { value: 'auth.refresh', labelKey: 'actions.authRefresh' },
  { value: 'auth.refresh_replay', labelKey: 'actions.authRefreshReplay' },
  { value: 'user.create', labelKey: 'actions.userCreate' },
  { value: 'user.update', labelKey: 'actions.userUpdate' },
  { value: 'user.delete', labelKey: 'actions.userDelete' },
  { value: 'role.create', labelKey: 'actions.roleCreate' },
  { value: 'role.update', labelKey: 'actions.roleUpdate' },
  { value: 'role.delete', labelKey: 'actions.roleDelete' },
  { value: 'menu.create', labelKey: 'actions.menuCreate' },
  { value: 'menu.update', labelKey: 'actions.menuUpdate' },
  { value: 'menu.reorder', labelKey: 'actions.menuReorder' },
  { value: 'menu.delete', labelKey: 'actions.menuDelete' },
  { value: 'file.upload', labelKey: 'actions.fileUpload' },
  { value: 'file.upload_avatar', labelKey: 'actions.fileUploadAvatar' },
  { value: 'file.delete', labelKey: 'actions.fileDelete' },
  { value: 'profile.update', labelKey: 'actions.profileUpdate' },
  {
    value: 'profile.change_password',
    labelKey: 'actions.profileChangePassword',
  },
  {
    value: 'profile.session_revoke',
    labelKey: 'actions.profileSessionRevoke',
  },
  {
    value: 'profile.sessions_revoke_others',
    labelKey: 'actions.profileSessionsRevokeOthers',
  },
] as const

export const AUDIT_RESOURCE_OPTIONS: readonly AuditCatalogOption[] = [
  { value: 'system', labelKey: 'resources.system' },
  { value: 'session', labelKey: 'resources.session' },
  { value: 'user', labelKey: 'resources.user' },
  { value: 'role', labelKey: 'resources.role' },
  { value: 'menu', labelKey: 'resources.menu' },
  { value: 'file', labelKey: 'resources.file' },
] as const

const ACTION_LABEL_KEY = new Map(
  AUDIT_ACTION_OPTIONS.map((option) => [option.value, option.labelKey])
)

const RESOURCE_LABEL_KEY = new Map(
  AUDIT_RESOURCE_OPTIONS.map((option) => [option.value, option.labelKey])
)

export function getAuditActionLabelKey(value: string): string | undefined {
  return ACTION_LABEL_KEY.get(value)
}

export function getAuditResourceLabelKey(value: string): string | undefined {
  return RESOURCE_LABEL_KEY.get(value)
}
