import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enAudit from './locales/en-US/audit.json'
import enAuth from './locales/en-US/auth.json'
import enCommon from './locales/en-US/common.json'
import enErrors from './locales/en-US/errors.json'
import enMenus from './locales/en-US/menus.json'
import enRoles from './locales/en-US/roles.json'
import enUsers from './locales/en-US/users.json'
import enValidation from './locales/en-US/validation.json'
import zhAudit from './locales/zh-CN/audit.json'
import zhAuth from './locales/zh-CN/auth.json'
import zhCommon from './locales/zh-CN/common.json'
import zhErrors from './locales/zh-CN/errors.json'
import zhMenus from './locales/zh-CN/menus.json'
import zhRoles from './locales/zh-CN/roles.json'
import zhUsers from './locales/zh-CN/users.json'
import zhValidation from './locales/zh-CN/validation.json'

const LANGUAGE_STORAGE_KEY = 'tianshu.lang'
const SUPPORTED_LANGUAGES = ['zh-CN', 'en-US'] as const
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number]

function resolveInitialLanguage(): SupportedLanguage {
  if (typeof window !== 'undefined') {
    const saved = window.localStorage.getItem(LANGUAGE_STORAGE_KEY)
    if (saved === 'zh-CN' || saved === 'en-US') {
      return saved
    }
    const navLang = window.navigator.language?.toLowerCase() ?? ''
    if (navLang.startsWith('en')) {
      return 'en-US'
    }
  }
  return 'zh-CN'
}

const initialLanguage = resolveInitialLanguage()

void i18n.use(initReactI18next).init({
  lng: initialLanguage,
  fallbackLng: 'zh-CN',
  supportedLngs: [...SUPPORTED_LANGUAGES],
  defaultNS: 'common',
  ns: [
    'common',
    'auth',
    'audit',
    'roles',
    'users',
    'menus',
    'errors',
    'validation',
  ],
  resources: {
    'zh-CN': {
      common: zhCommon,
      auth: zhAuth,
      audit: zhAudit,
      roles: zhRoles,
      users: zhUsers,
      menus: zhMenus,
      errors: zhErrors,
      validation: zhValidation,
    },
    'en-US': {
      common: enCommon,
      auth: enAuth,
      audit: enAudit,
      roles: enRoles,
      users: enUsers,
      menus: enMenus,
      errors: enErrors,
      validation: enValidation,
    },
  },
  interpolation: {
    escapeValue: false,
  },
})

if (typeof document !== 'undefined') {
  document.documentElement.lang = initialLanguage
}

export async function changeAppLanguage(lng: SupportedLanguage): Promise<void> {
  await i18n.changeLanguage(lng)
  if (typeof document !== 'undefined') {
    document.documentElement.lang = lng
  }
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(LANGUAGE_STORAGE_KEY, lng)
  }
}

export function getCurrentLanguage(): SupportedLanguage {
  return i18n.language === 'en-US' ? 'en-US' : 'zh-CN'
}

export default i18n
