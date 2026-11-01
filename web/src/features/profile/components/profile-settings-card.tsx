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
import { Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import type { UserProfile } from '../types'
import { NotificationTab } from './tabs/notification-tab'

// ============================================================================
// Profile Settings Card Component
// ============================================================================

interface ProfileSettingsCardProps {
  profile: UserProfile | null
  loading: boolean
  onProfileUpdate: () => void
  quotaRemindEnabled: boolean
  quotaRemindThreshold: number
}

export function ProfileSettingsCard({
  profile,
  loading,
  onProfileUpdate,
  quotaRemindEnabled,
  quotaRemindThreshold,
}: ProfileSettingsCardProps) {
  const { t } = useTranslation()

  if (loading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-4 !pb-3'>
          <Skeleton className='h-5 w-32' />
        </CardHeader>
        <CardContent className='space-y-4 p-4'>
          {['notifications', 'threshold', 'preferences'].map((key) => (
            <Skeleton key={key} className='h-12 w-full' />
          ))}
        </CardContent>
      </Card>
    )
  }

  return (
    <TitledCard
      title={t('Settings & Preferences')}
      icon={<Settings className='size-4' />}
      iconTone='info'
      disableHoverEffect
      className='min-w-0 shadow-none'
      headerClassName='p-4 !pb-3 sm:p-4 sm:!pb-3'
      contentClassName='p-4 sm:p-4'
      titleClassName='text-sm sm:text-sm'
      iconClassName='size-7 sm:size-7'
    >
      <NotificationTab
        profile={profile}
        onUpdate={onProfileUpdate}
        quotaRemindEnabled={quotaRemindEnabled}
        quotaRemindThreshold={quotaRemindThreshold}
      />
    </TitledCard>
  )
}
