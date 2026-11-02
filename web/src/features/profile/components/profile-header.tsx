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

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { UserAvatar } from '@/components/user-avatar'
import { getRoleLabel } from '@/lib/roles'

import { getDisplayName } from '../lib'
import type { UserProfile } from '../types'

interface ProfileHeaderProps {
  profile: UserProfile | null
  loading: boolean
}

export function ProfileHeader(props: ProfileHeaderProps) {
  const { t } = useTranslation()
  const profile = props.profile
  if (!props.loading && !profile) return null

  return (
    <section aria-label={t('Profile')} aria-busy={props.loading}>
      <Card data-card-hover='false' className='gap-0 py-0 shadow-none'>
        <CardContent className='min-w-0 p-4 sm:p-5'>
          <div className='flex min-w-0 flex-1 items-center gap-3'>
            {props.loading ? (
              <>
                <Skeleton className='size-14 shrink-0 rounded-full' />
                <div className='min-w-0 space-y-2'>
                  <Skeleton className='h-6 w-36' />
                  <Skeleton className='h-4 w-44' />
                  <Skeleton className='h-4 w-24' />
                </div>
              </>
            ) : (
              profile && (
                <>
                  <UserAvatar
                    userId={profile.id}
                    avatarUrl={profile.avatar_url}
                    className='size-14 shrink-0'
                  />
                  <div className='min-w-0 space-y-1.5'>
                    <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
                      <h3 className='min-w-0 text-xl leading-snug font-semibold tracking-tight [overflow-wrap:anywhere]'>
                        {getDisplayName(profile)}
                      </h3>
                      <StatusBadge
                        label={getRoleLabel(profile.role)}
                        variant='neutral'
                        copyable={false}
                        className='text-xs'
                      />
                    </div>
                    <p className='text-muted-foreground text-sm [overflow-wrap:anywhere]'>
                      @{profile.username}
                      {profile.group && (
                        <span className='ml-2 text-xs'>· {profile.group}</span>
                      )}
                    </p>
                    {profile.email && (
                      <p className='text-muted-foreground text-xs [overflow-wrap:anywhere]'>
                        {profile.email}
                      </p>
                    )}
                    <CopyButton
                      value={String(profile.id)}
                      size='sm'
                      className='text-muted-foreground h-6 gap-1.5 px-0 text-xs'
                      aria-label={t('User ID')}
                    >
                      {t('User ID')} {profile.id}
                    </CopyButton>
                  </div>
                </>
              )
            )}
          </div>
        </CardContent>
      </Card>
    </section>
  )
}
