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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getReleaseState, releaseStateLabelKeys } from '../lib/release-state'
import type { DesktopRelease } from '../types'

export function ReleaseStatus(props: { release: DesktopRelease }) {
  const { t } = useTranslation()
  const state = getReleaseState(props.release)
  let variant: 'default' | 'secondary' | 'warning' | 'outline' = 'secondary'
  if (state === 'stable') variant = 'default'
  if (state === 'rollout') variant = 'outline'
  if (state === 'paused') variant = 'warning'
  return <Badge variant={variant}>{t(releaseStateLabelKeys[state])}</Badge>
}

export function ReleaseScope(props: { release: DesktopRelease }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const release = props.release
  if (release.status === 0) return <span>{t('Not distributing')}</span>
  if (release.status === 1) {
    return <span>{t('All stable-channel devices')}</span>
  }
  return (
    <span>
      {t('Preview devices: {{percentage}}%', {
        percentage: formatNumber(release.rollout_percentage, locale),
      })}
      {release.device_mid_allowlist.length > 0 && (
        <span className='text-muted-foreground block text-xs'>
          {t('Plus {{devices}} allowlisted devices', {
            devices: formatNumber(release.device_mid_allowlist.length, locale),
          })}
        </span>
      )}
      {release.rollout_disabled && (
        <span className='text-muted-foreground block text-xs'>
          {t('Paused, including the allowlist')}
        </span>
      )}
    </span>
  )
}
