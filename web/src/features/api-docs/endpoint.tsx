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
import { StaticDataTable } from '@/components/data-table'
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { CodeSamples } from './code-samples'
import type { ApiDoc } from './reference'

export function ApiEndpoint(props: { doc: ApiDoc }) {
  const { t } = useTranslation()
  const doc = props.doc
  const url = `${window.location.origin}${doc.path}`

  return (
    <div className='space-y-10'>
      <div className='bg-muted/30 flex min-w-0 items-center gap-3 rounded-lg border px-4 py-3'>
        <span className='text-primary shrink-0 font-mono text-xs font-semibold'>
          {doc.method}
        </span>
        <code className='min-w-0 flex-1 text-sm break-all'>{url}</code>
        <CopyButton value={url} aria-label={t('Copy API URL')} />
      </div>
      {doc.parameters && (
        <section id='parameters' className='scroll-mt-24 space-y-4'>
          <h2 className='text-xl font-semibold'>{t('Request parameters')}</h2>
          <StaticDataTable>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Parameter')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Description')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {doc.parameters.map((parameter) => (
                <TableRow key={parameter.name}>
                  <TableCell className='align-top'>
                    <code>{parameter.name}</code>
                    {parameter.required && (
                      <span className='text-primary mt-1 block text-xs'>
                        {t('Required')}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className='align-top font-mono text-xs'>
                    {parameter.type}
                  </TableCell>
                  <TableCell className='min-w-48 align-top leading-6 whitespace-normal'>
                    {t(parameter.description)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </StaticDataTable>
        </section>
      )}
      {doc.request && (
        <section id='request' className='scroll-mt-24 space-y-4'>
          <h2 className='text-xl font-semibold'>{t('Request example')}</h2>
          <CodeSamples samples={doc.request} />
        </section>
      )}
      {doc.response && (
        <section id='response' className='scroll-mt-24 space-y-4'>
          <h2 className='text-xl font-semibold'>{t('Response example')}</h2>
          <CodeSamples samples={[{ language: 'json', code: doc.response }]} />
        </section>
      )}
      {doc.stream && (
        <section id='streaming' className='scroll-mt-24 space-y-4'>
          <h2 className='text-xl font-semibold'>{t('Streaming')}</h2>
          <p className='text-muted-foreground leading-7'>
            {t(doc.stream.description)}
          </p>
          <CodeSamples
            samples={[{ language: 'bash', code: doc.stream.request }]}
          />
          <CodeSamples
            samples={[{ language: 'text', code: doc.stream.response }]}
          />
        </section>
      )}
      {doc.related && (
        <section id='related' className='scroll-mt-24 space-y-4'>
          <h2 className='text-xl font-semibold'>{t('Related endpoints')}</h2>
          <div className='divide-y rounded-lg border'>
            {doc.related.map((endpoint) => (
              <div
                key={`${endpoint.method}:${endpoint.path}`}
                className='space-y-2 p-4'
              >
                <code className='block text-sm break-all'>
                  <span className='text-primary'>{endpoint.method}</span>{' '}
                  {endpoint.path}
                </code>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(endpoint.description)}
                </p>
              </div>
            ))}
          </div>
        </section>
      )}
      {doc.notes && (
        <section id='notes' className='scroll-mt-24 space-y-4'>
          <h2 className='text-xl font-semibold'>{t('Usage notes')}</h2>
          <ul className='text-muted-foreground list-disc space-y-3 pl-5 leading-7'>
            {doc.notes.map((note) => (
              <li key={note}>{t(note)}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
