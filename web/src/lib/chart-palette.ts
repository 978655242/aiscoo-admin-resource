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
/**
 * Official-site chart palette: deep teal first, then the scene-card hues.
 * Registered on VChart's light theme so every chart picks it up.
 */
export const CHART_PALETTE = [
  '#1b6f86',
  '#3a6ed8',
  '#8b5cf6',
  '#e2804c',
  '#469a88',
  '#46dcec',
  '#8a3a25',
  '#1b3473',
]

type ThemeManagerLike = {
  getTheme: (name: string) => unknown
  registerTheme: (name: string, theme: Record<string, unknown>) => void
  setCurrentTheme: (name: string) => void
}

/** Apply the palette to VChart's `light` theme and make it current. */
export function applyOfficialChartTheme(ThemeManager: ThemeManagerLike) {
  const base = (ThemeManager.getTheme('light') ?? {}) as {
    colorScheme?: Record<string, unknown>
  }
  ThemeManager.registerTheme('light', {
    ...base,
    colorScheme: { ...base.colorScheme, default: CHART_PALETTE },
  })
  ThemeManager.setCurrentTheme('light')
}
