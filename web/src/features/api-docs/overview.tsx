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
import { API_CAPABILITIES, API_DOCS } from './reference'

export function ApiOverview() {
  const { t } = useTranslation()
  const origin = window.location.origin
  const apiEntries = [
    {
      api: t('OpenAI-compatible API'),
      url: `${origin}/v1`,
      clients: t('OpenAI SDK, LangChain, and most agent frameworks'),
    },
    {
      api: t('Anthropic-compatible API'),
      url: origin,
      clients: t('Anthropic SDK, Claude Code'),
    },
  ]

  return (
    <div className='space-y-10'>
      <section id='base-url' className='scroll-mt-24 space-y-4'>
        <h2 id='base-url-title' className='text-xl font-semibold'>
          {t('Base URL')}
        </h2>
        <StaticDataTable
          tableClassName='min-w-[36rem] table-fixed'
          tableProps={{ 'aria-labelledby': 'base-url-title' }}
        >
          <TableHeader className='bg-muted/50'>
            <TableRow>
              <TableHead scope='col' className='w-1/4 border-r px-4'>
                {t('API')}
              </TableHead>
              <TableHead scope='col' className='w-2/5 border-r px-4'>
                {t('API Base URL')}
              </TableHead>
              <TableHead scope='col' className='px-4'>
                {t('Common clients')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {apiEntries.map((entry) => (
              <TableRow key={entry.api}>
                <TableCell className='border-r px-4 py-3 whitespace-normal'>
                  {entry.api}
                </TableCell>
                <TableCell className='border-r px-4 py-3 whitespace-normal'>
                  <div className='flex min-w-0 items-center justify-between gap-2'>
                    <code className='bg-muted min-w-0 rounded px-2 py-1 break-all'>
                      {entry.url}
                    </code>
                    <CopyButton
                      value={entry.url}
                      aria-label={`${t('Copy API URL')}: ${entry.url}`}
                    />
                  </div>
                </TableCell>
                <TableCell className='px-4 py-3 whitespace-normal'>
                  {entry.clients}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </StaticDataTable>
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
        <h2 id='endpoints-title' className='text-xl font-semibold'>
          {t('Model API endpoints')}
        </h2>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Replace YOUR_MODEL_ID with an id returned by GET /v1/models. Endpoint and parameter support depend on the selected model and its upstream provider.'
          )}
        </p>
        <p className='text-muted-foreground leading-7'>
          {t(
            'Choose a capability below, then open its reference for parameters, examples, and limitations. Some capabilities share an endpoint; media tasks also require an enabled compatible plugin and channel.'
          )}
        </p>
        <StaticDataTable
          tableClassName='min-w-[40rem]'
          tableProps={{ 'aria-label': t('Model capabilities') }}
        >
          <TableHeader>
            <TableRow>
              <TableHead scope='col'>{t('Capability')}</TableHead>
              <TableHead scope='col'>{t('Description')}</TableHead>
              <TableHead scope='col'>{t('API reference')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {API_CAPABILITIES.map((capability) => (
              <TableRow key={capability.title}>
                <TableCell className='align-top whitespace-normal'>
                  {t(capability.title)}
                </TableCell>
                <TableCell className='max-w-80 align-top whitespace-normal'>
                  {t(capability.description)}
                </TableCell>
                <TableCell className='align-top whitespace-normal'>
                  <div className='flex flex-col items-start gap-2'>
                    {capability.docs.map((id) => {
                      const doc = API_DOCS.find((item) => item.id === id)
                      if (!doc) return null

                      return (
                        <Link
                          key={id}
                          to='/docs/$page'
                          params={{ page: id }}
                          className='text-primary hover:underline'
                        >
                          {t(doc.title)}
                        </Link>
                      )
                    })}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </StaticDataTable>
        <h3 className='text-lg font-semibold'>{t('Endpoint directory')}</h3>
        <StaticDataTable
          tableClassName='min-w-[48rem]'
          tableProps={{ 'aria-labelledby': 'endpoints-title' }}
        >
          <TableHeader>
            <TableRow>
              <TableHead>{t('API')}</TableHead>
              <TableHead>{t('Method')}</TableHead>
              <TableHead>{t('Endpoint')}</TableHead>
              <TableHead>{t('Purpose')}</TableHead>
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
                <TableCell className='max-w-md min-w-64 leading-6 whitespace-normal'>
                  {t(doc.summary)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </StaticDataTable>
        <Alert>
          <AlertDescription>
            {t(
              'File management, fine-tuning, image variations, and standalone token-count endpoints are not implemented.'
            )}
          </AlertDescription>
        </Alert>
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
