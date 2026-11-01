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
