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
import type { ReactNode } from 'react'
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import type { ReleaseCreateFormValues } from '../lib/release-form'
import { ReleaseNotesEditor } from './release-notes-editor'

export function ReleaseCreateFields(props: {
  disabled: boolean
  children: ReactNode
}) {
  const { t } = useTranslation()
  const form = useFormContext<ReleaseCreateFormValues>()

  return (
    <>
      <SideDrawerSection>
        <SideDrawerSectionHeader
          title={t('Release information')}
          description={t(
            'This release is for macOS on Apple Silicon (darwin-aarch64).'
          )}
        />
        <FormField
          control={form.control}
          name='version'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Version')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder={t('1.2.3 or 1.2.3-beta.1')}
                  disabled={props.disabled}
                  autoComplete='off'
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Use X.Y.Z with an optional prerelease suffix. Build metadata (+...) is not supported.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='minimum_system_version'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Minimum system version')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder={t('22.0.0')}
                  disabled={props.disabled}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Use the Darwin kernel version: 22.0.0 corresponds to macOS 13.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='release_date'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Release date (optional)')}</FormLabel>
              <FormControl>
                <fieldset
                  disabled={props.disabled}
                  className='min-w-0'
                  aria-label={t('Release date (optional)')}
                >
                  <DateTimePicker
                    value={
                      field.value > 0 ? new Date(field.value * 1000) : undefined
                    }
                    onChange={(date) =>
                      field.onChange(
                        date ? Math.floor(date.getTime() / 1000) : 0
                      )
                    }
                    placeholder={t('Use registration time')}
                    className='[&_input[type=time]]:w-24'
                  />
                </fieldset>
              </FormControl>
              <FormDescription>
                {t('Leave empty to use the time this release is registered.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </SideDrawerSection>
      {props.children}
      <SideDrawerSection>
        <SideDrawerSectionHeader
          title={t('Release notes')}
          description={t(
            'Add Markdown notes in Simplified Chinese and English. The combined JSON must not exceed 64 KiB.'
          )}
        />
        <FormField
          control={form.control}
          name='release_notes_by_locale'
          render={({ field }) => (
            <FormItem>
              <FormLabel className='sr-only'>{t('Release notes')}</FormLabel>
              <FormControl>
                <ReleaseNotesEditor
                  value={field.value}
                  onChange={field.onChange}
                  disabled={props.disabled}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </SideDrawerSection>
    </>
  )
}
