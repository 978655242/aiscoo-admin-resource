/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { createContext, useContext, useEffect } from 'react'

import { readZenPlanDisplayContext } from '@/features/zen/display-context'

// The admin UI uses a single light theme. Keys written by earlier builds are
// cleared once so they cannot linger.
const LEGACY_THEME_STORAGE_KEYS = [
  'newapi:theme:v1:mode',
  'newapi:theme:v1:preset',
  'newapi:theme:v1:font',
  'newapi:theme:v1:radius',
  'newapi:theme:v1:scale',
  'newapi:theme:v1:content-layout',
]

type ThemeProviderState = {
  resolvedTheme: 'light' | 'dark'
}

const ThemeContext = createContext<ThemeProviderState>({
  resolvedTheme: 'light',
})

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  // ZEN: 全局浅色主题曾覆盖桌面传入的深色；仅套餐页使用经过验证的展示上下文。
  const resolvedTheme = readZenPlanDisplayContext()?.theme ?? 'light'
  useEffect(() => {
    const root = document.documentElement
    root.classList.remove('light', 'dark')
    root.classList.add(resolvedTheme)
    root.style.colorScheme = resolvedTheme
    try {
      LEGACY_THEME_STORAGE_KEYS.forEach((key) =>
        window.localStorage.removeItem(key)
      )
    } catch {
      // Storage can be blocked; the theme does not depend on it.
    }
  }, [resolvedTheme])

  return <ThemeContext value={{ resolvedTheme }}>{children}</ThemeContext>
}

// eslint-disable-next-line react-refresh/only-export-components
export const useTheme = () => useContext(ThemeContext)
