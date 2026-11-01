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
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { useFieldArray, useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import type { ReleaseCreateFormValues } from '../lib/release-form'

export function ReleaseCreateFiles(props: { disabled: boolean }) {
  const { t } = useTranslation()
  const form = useFormContext<ReleaseCreateFormValues>()
  const files = useFieldArray({ control: form.control, name: 'files' })
  const countError =
    form.formState.errors.files?.root?.message ??
    form.formState.errors.files?.message

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Release files')}
        description={t(
          'Add 1–8 files. The first file is the primary download. All download URLs must use HTTPS.'
        )}
      />
      {countError && (
        <p role='alert' className='text-destructive text-sm'>
          {t(countError)}
        </p>
      )}
      {files.fields.map((file, index) => (
        <fieldset
          key={file.id}
          disabled={props.disabled}
          className='min-w-0 rounded-lg border p-3'
        >
          <legend className='px-1 text-sm font-medium'>
            {index === 0
              ? t('File {{number}} · Primary', { number: index + 1 })
              : t('File {{number}}', { number: index + 1 })}
          </legend>
          <div className='mb-3 flex justify-end gap-1'>
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              aria-label={t('Move file {{number}} up', { number: index + 1 })}
              disabled={props.disabled || index === 0}
              onClick={() => files.move(index, index - 1)}
            >
              <ArrowUp aria-hidden='true' />
            </Button>
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              aria-label={t('Move file {{number}} down', { number: index + 1 })}
              disabled={props.disabled || index === files.fields.length - 1}
              onClick={() => files.move(index, index + 1)}
            >
              <ArrowDown aria-hidden='true' />
            </Button>
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              aria-label={t('Remove file {{number}}', { number: index + 1 })}
              disabled={props.disabled || files.fields.length === 1}
              onClick={() => files.remove(index)}
            >
              <Trash2 aria-hidden='true' />
            </Button>
          </div>
          <div className='grid gap-3'>
            <FormField
              control={form.control}
              name={`files.${index}.url`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Download URL')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type='url'
                      placeholder={t('https://downloads.example.com/app.zip')}
                      disabled={props.disabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name={`files.${index}.sha512`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('SHA-512 (Base64)')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      spellCheck={false}
                      autoComplete='off'
                      placeholder={t('Base64-encoded 64-byte digest')}
                      disabled={props.disabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name={`files.${index}.size`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('File size (bytes)')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      inputMode='numeric'
                      placeholder={t('Positive integer in bytes')}
                      disabled={props.disabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </fieldset>
      ))}
      <Button
        type='button'
        variant='outline'
        className='self-start'
        disabled={props.disabled || files.fields.length >= 8}
        onClick={() => files.append({ url: '', sha512: '', size: '' })}
      >
        <Plus aria-hidden='true' />
        {t('Add file')}
      </Button>
    </SideDrawerSection>
  )
}
