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
import { ExternalLink, Loader2 } from 'lucide-react'
import type { ReactElement } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface RedemptionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  trigger: ReactElement
  code: string
  onCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  topupLink?: string
}

export function RedemptionDialog(props: RedemptionDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!props.redeeming) props.onOpenChange(open)
      }}
      trigger={props.trigger}
      title={t('Redeem')}
      description={t('Enter your redemption code')}
      contentClassName='sm:max-w-md'
      footer={
        <Button
          type='submit'
          form='wallet-redemption-form'
          disabled={props.redeeming || !props.code.trim()}
          className='w-full sm:w-auto'
        >
          {props.redeeming && (
            <Loader2 aria-hidden='true' className='animate-spin' />
          )}
          {t('Redeem')}
        </Button>
      }
    >
      <form
        id='wallet-redemption-form'
        onSubmit={(event) => {
          event.preventDefault()
          if (!props.redeeming && props.code.trim()) props.onRedeem()
        }}
        className='space-y-3'
      >
        <Label htmlFor='redemption-code'>{t('Redemption Code')}</Label>
        <Input
          id='redemption-code'
          value={props.code}
          onChange={(event) => props.onCodeChange(event.target.value)}
          placeholder={t('Enter your redemption code')}
          disabled={props.redeeming}
          autoComplete='off'
        />
        {props.topupLink && (
          <p className='text-muted-foreground text-xs'>
            {t('Need a redemption code?')}{' '}
            <a
              href={props.topupLink}
              target='_blank'
              rel='noopener noreferrer'
              className='inline-flex items-center gap-1 underline-offset-4 hover:underline'
            >
              {t('Get one here')}
              <ExternalLink aria-hidden='true' className='size-3' />
            </a>
          </p>
        )}
      </form>
    </Dialog>
  )
}
