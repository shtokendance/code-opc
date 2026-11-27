import type { LocaleAdapter, SupportedLocale } from "@multica/core/i18n";

const STORAGE_KEY = "multica-locale";

// Desktop adapter:
//   - User choice: localStorage (set by Settings switcher).
//   - System preference: locale main injected via additionalArguments
//     (read from preload, exposed on window.desktopAPI.systemLocale).
//   - Persist: localStorage. The Settings switcher additionally PATCHes
//     /api/me when logged in so user.language follows the user across devices.
export function createDesktopLocaleAdapter(_systemLocale: string): LocaleAdapter {
  return {
    getUserChoice() {
      try {
        return window.localStorage.getItem(STORAGE_KEY);
      } catch {
        return null;
      }
    },
    getSystemPreferences() {
      // Default to Simplified Chinese on initial launch even if OS locale is English.
      return ["zh-Hans"];
    },
    persist(locale: SupportedLocale) {
      try {
        window.localStorage.setItem(STORAGE_KEY, locale);
      } catch {
        // Best-effort
      }
    },
  };
}
