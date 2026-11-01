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
import { WalletCards } from 'lucide-react'
import { useId, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { formatNumber, formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
  actions?: ReactNode
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t } = useTranslation()
  const titleId = useId()

  return (
    <section
      aria-labelledby={titleId}
      aria-busy={props.loading}
      className='bg-card text-card-foreground rounded-2xl border p-5 sm:p-6'
    >
      <div className='flex flex-wrap items-center justify-between gap-4'>
        <div className='flex items-center gap-2.5'>
          <IconBadge tone='success' size='stat'>
            <WalletCards />
          </IconBadge>
          <h3 id={titleId} className='text-sm font-medium'>
            {t('Current Balance')}
          </h3>
        </div>
        {props.actions && (
          <div className='flex flex-wrap items-center gap-2'>
            {props.actions}
          </div>
        )}
      </div>

      <div className='mt-6 flex flex-col gap-6 sm:flex-row sm:items-end sm:justify-between'>
        <div className='min-w-0'>
          {props.loading ? (
            <Skeleton className='h-12 w-48' />
          ) : (
            <div className='text-4xl font-semibold tracking-tight break-all tabular-nums sm:text-5xl'>
              {formatQuota(props.user?.quota ?? 0)}
            </div>
          )}
          <p className='text-muted-foreground mt-2 text-xs'>
            {t('Remaining quota')}
          </p>
        </div>

        <dl className='grid grid-cols-2 gap-6 border-t pt-4 sm:gap-10 sm:border-t-0 sm:pt-0'>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('Total Usage')}
            </dt>
            <dd className='mt-2 text-base font-medium break-all tabular-nums'>
              {props.loading ? (
                <Skeleton className='h-6 w-20' />
              ) : (
                formatQuota(props.user?.used_quota ?? 0)
              )}
            </dd>
          </div>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('API Requests')}
            </dt>
            <dd className='mt-2 text-base font-medium break-all tabular-nums'>
              {props.loading ? (
                <Skeleton className='h-6 w-20' />
              ) : (
                formatNumber(props.user?.request_count ?? 0)
              )}
            </dd>
          </div>
        </dl>
      </div>
    </section>
  )
}
