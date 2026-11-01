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

import { StaticDataTable } from '@/components/data-table'
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { CodeSamples } from './code-samples'

const ERRORS = [
  ['400', 'Invalid JSON, missing model, or unsupported request parameters.'],
  ['401', 'Missing, invalid, disabled, expired, or exhausted API key.'],
  [
    '403',
    'Model access, IP restrictions, or available quota prevented the request.',
  ],
  ['404', 'Check the endpoint path and avoid repeating /v1.'],
  ['413', 'The request body exceeds the server limit.'],
  [
    '429',
    'Request rate limit reached. Retry with exponential backoff and jitter.',
  ],
  [
    '500 / 502 / 503',
    'The gateway or upstream is unavailable, or no route can serve this model.',
  ],
] as const

export function ApiErrors() {
  const { t } = useTranslation()

  return (
    <div className='space-y-10'>
      <section id='error-response' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('Error response')}</h2>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Inspect both the HTTP status and error.message. The following JSON illustrates a gateway authentication error; upstream and protocol-specific errors can have different fields.'
          )}
        </p>
        <CodeSamples
          samples={[
            {
              language: 'json',
              code: JSON.stringify(
                {
                  error: {
                    message: 'Invalid token (request id: ...)',
                    type: 'new_api_error',
                    code: '',
                  },
                },
                null,
                2
              ),
            },
          ]}
        />
      </section>
      <section id='status-codes' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('HTTP status codes')}</h2>
        <StaticDataTable>
          <TableHeader>
            <TableRow>
              <TableHead>HTTP</TableHead>
              <TableHead>{t('Description')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {ERRORS.map(([status, description]) => (
              <TableRow key={status}>
                <TableCell className='font-mono'>{status}</TableCell>
                <TableCell className='leading-6 whitespace-normal'>
                  {t(description)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </StaticDataTable>
      </section>
      <section id='request-id' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('Request ID and retries')}</h2>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Save the X-Oneapi-Request-Id response header, request time, endpoint, and model when investigating a failed call. Remove API keys and sensitive request content before sharing logs.'
          )}
        </p>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Retry transient failures with a bounded backoff. After a timeout or interrupted stream, verify the result before resubmitting: generation requests are not guaranteed to be idempotent.'
          )}
        </p>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Once a stream has started, later errors may arrive as events rather than a new HTTP status. Handle error events, connection failures, and incomplete output in your client.'
          )}
        </p>
      </section>
    </div>
  )
}
