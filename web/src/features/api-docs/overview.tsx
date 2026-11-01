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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { CodeSamples } from './code-samples'
import { API_DOCS } from './reference'

export function ApiOverview() {
  const { t } = useTranslation()
  const origin = window.location.origin

  return (
    <div className='space-y-10'>
      <section id='base-url' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('Base URL')}</h2>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Use the current site origin for HTTP requests. OpenAI SDK clients use the origin followed by /v1; Anthropic clients use the origin without /v1.'
          )}
        </p>
        <div className='bg-muted/30 flex min-w-0 items-center justify-between gap-3 rounded-lg border px-4 py-3'>
          <code className='min-w-0 text-sm break-all'>{`${origin}/v1`}</code>
          <CopyButton value={`${origin}/v1`} aria-label={t('Copy API URL')} />
        </div>
        <Alert>
          <AlertDescription>
            {t(
              'Keep the port in the URL when one is present. Do not append /v1 twice. API requests containing credentials should use HTTPS.'
            )}
          </AlertDescription>
        </Alert>
      </section>
      <section id='authentication' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('Authentication')}</h2>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Send your gateway API key with every request, including model listing. Provider keys and dashboard access tokens cannot replace a gateway API key.'
          )}
        </p>
        <CodeSamples
          samples={[
            {
              language: 'bash',
              code: 'export API_KEY="YOUR_API_KEY"\n\ncurl "__API_ORIGIN__/v1/models" \\\n  -H "Authorization: Bearer $API_KEY"',
            },
          ]}
        />
        <p className='text-muted-foreground leading-7'>
          {t(
            'For Anthropic requests, send x-api-key and anthropic-version. For Gemini requests, send x-goog-api-key. Keep API keys on your server or in environment variables, outside browser code and source control.'
          )}
        </p>
      </section>
      <section id='endpoints' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('Model API endpoints')}</h2>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Replace YOUR_MODEL_ID with an id returned by GET /v1/models. Endpoint and parameter support depend on the selected model and its upstream provider.'
          )}
        </p>
        <StaticDataTable>
          <TableHeader>
            <TableRow>
              <TableHead>{t('API')}</TableHead>
              <TableHead>{t('Method')}</TableHead>
              <TableHead>{t('Endpoint')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {API_DOCS.map((doc) => (
              <TableRow key={doc.id}>
                <TableCell>
                  <Link
                    to='/docs/$page'
                    params={{ page: doc.id }}
                    className='text-primary hover:underline'
                  >
                    {t(doc.title)}
                  </Link>
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {doc.method}
                </TableCell>
                <TableCell>
                  <code className='text-xs'>{doc.path}</code>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </StaticDataTable>
      </section>
      <section id='first-request' className='scroll-mt-24 space-y-4'>
        <h2 className='text-xl font-semibold'>{t('Minimal request')}</h2>
        <CodeSamples
          samples={[
            {
              language: 'bash',
              code: 'curl "__API_ORIGIN__/v1/chat/completions" \\\n  -H "Authorization: Bearer $API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d \'{\n    "model": "YOUR_MODEL_ID",\n    "messages": [{"role": "user", "content": "Hello"}]\n  }\'',
            },
          ]}
        />
        <p className='text-muted-foreground leading-7'>
          {t(
            'Successful chat responses contain choices[].message.content. Set stream to true to receive incremental output; see the endpoint reference for its event format.'
          )}
        </p>
      </section>
    </div>
  )
}
