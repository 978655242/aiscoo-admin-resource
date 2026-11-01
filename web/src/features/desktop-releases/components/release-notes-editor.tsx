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
import { useId, useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { Markdown } from '@/components/ui/markdown'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { cn } from '@/lib/utils'

import type { ReleaseNotes, ReleaseNotesLocale } from '../types'

type ReleaseNotesEditorProps = Omit<
  ComponentProps<'fieldset'>,
  'onChange' | 'value'
> & {
  value: ReleaseNotes
  onChange: (notes: ReleaseNotes) => void
  disabled?: boolean
}

const locales: ReleaseNotesLocale[] = ['zh-CN', 'en-US']

export function ReleaseNotesEditor(props: ReleaseNotesEditorProps) {
  const { t } = useTranslation()
  const [locale, setLocale] = useState<ReleaseNotesLocale>('zh-CN')
  const [mode, setMode] = useState('edit')
  const editorId = useId()
  const { value, onChange, disabled, className, ...controlProps } = props

  return (
    <fieldset
      {...controlProps}
      disabled={disabled}
      tabIndex={-1}
      className={cn('min-w-0', className)}
    >
      <legend className='sr-only'>{t('Release notes')}</legend>
      <Tabs
        value={locale}
        onValueChange={(next) => {
          if (next === 'zh-CN' || next === 'en-US') setLocale(next)
        }}
      >
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <TabsList aria-label={t('Release notes language')}>
            <TabsTrigger value='zh-CN' disabled={disabled}>
              {t('Simplified Chinese')}
            </TabsTrigger>
            <TabsTrigger value='en-US' disabled={disabled}>
              {t('English')}
            </TabsTrigger>
          </TabsList>
          <ToggleGroup
            aria-label={t('Release notes view')}
            value={[mode]}
            onValueChange={(next) => {
              if (next[0]) setMode(next[0])
            }}
            disabled={disabled}
            variant='outline'
            size='sm'
          >
            <ToggleGroupItem value='edit'>{t('Edit')}</ToggleGroupItem>
            <ToggleGroupItem value='preview'>{t('Preview')}</ToggleGroupItem>
          </ToggleGroup>
        </div>
        {locales.map((language) => (
          <TabsContent key={language} value={language}>
            {mode === 'edit' ? (
              <Textarea
                id={`${editorId}-${language}`}
                value={value[language] ?? ''}
                onChange={(event) =>
                  onChange({ ...value, [language]: event.target.value })
                }
                aria-label={t('Release notes ({{language}})', {
                  language:
                    language === 'zh-CN'
                      ? t('Simplified Chinese')
                      : t('English'),
                })}
                aria-invalid={props['aria-invalid']}
                aria-describedby={props['aria-describedby']}
                placeholder={t('Write release notes in Markdown')}
                className='min-h-52 resize-y'
                disabled={disabled}
              />
            ) : (
              <div className='min-h-52 overflow-x-auto rounded-md border p-4'>
                {value[language]?.trim() ? (
                  <Markdown>{value[language] ?? ''}</Markdown>
                ) : (
                  <EmptyState
                    title={t('No release notes to preview')}
                    className='min-h-40'
                  />
                )}
              </div>
            )}
          </TabsContent>
        ))}
      </Tabs>
    </fieldset>
  )
}
