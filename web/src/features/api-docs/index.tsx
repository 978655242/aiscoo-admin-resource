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
import { ArrowLeft, ArrowRight, BookOpen, ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { useMediaQuery } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

import { ApiEndpoint } from './endpoint'
import { ApiErrors } from './errors'
import { ApiOverview } from './overview'
import { API_DOCS } from './reference'

const PAGES = [
  { id: 'overview', title: 'API overview', group: 'API basics' },
  ...API_DOCS,
  { id: 'errors', title: 'Errors and retries', group: 'API basics' },
]
const GROUPS = ['API basics', 'Text and multimodal', 'Media and utilities']

export function ApiDocs(props: { page?: string }) {
  const { t } = useTranslation()
  const [menuOpen, setMenuOpen] = useState(false)
  const desktop = useMediaQuery('(min-width: 1024px)')
  const pageId = props.page ?? 'overview'
  const pageIndex = PAGES.findIndex((page) => page.id === pageId)
  const page = PAGES[pageIndex] ?? PAGES[0]
  const doc = API_DOCS.find((item) => item.id === pageId)
  const previous = PAGES[pageIndex - 1]
  const next = PAGES[pageIndex + 1]
  const sections = []

  if (pageId === 'overview') {
    sections.push(
      ['base-url', 'Base URL'],
      ['authentication', 'Authentication'],
      ['endpoints', 'Model API endpoints'],
      ['first-request', 'Minimal request']
    )
  } else if (pageId === 'errors') {
    sections.push(
      ['error-response', 'Error response'],
      ['status-codes', 'HTTP status codes'],
      ['request-id', 'Request ID and retries']
    )
  } else if (doc) {
    if (doc.parameters) sections.push(['parameters', 'Request parameters'])
    if (doc.request) sections.push(['request', 'Request example'])
    if (doc.response) sections.push(['response', 'Response example'])
    if (doc.stream) sections.push(['streaming', 'Streaming'])
    if (doc.related) sections.push(['related', 'Related endpoints'])
    if (doc.notes) sections.push(['notes', 'Usage notes'])
  }

  return (
    <PublicLayout showMainContainer={false} showNotifications={false}>
      <div className='mx-auto max-w-[1440px] px-4 pt-20 sm:px-6 lg:grid lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-10 xl:grid-cols-[220px_minmax(0,1fr)_180px] xl:gap-12'>
        <aside
          className='lg:sticky lg:top-24 lg:max-h-[calc(100svh-7rem)] lg:overflow-y-auto lg:pb-8'
          aria-label={t('API documentation navigation')}
        >
          <Collapsible open={desktop || menuOpen} onOpenChange={setMenuOpen}>
            <CollapsibleTrigger
              render={
                <Button
                  variant='outline'
                  className='mb-5 w-full justify-between lg:hidden'
                />
              }
            >
              <BookOpen aria-hidden='true' className='size-4' />
              {t('API documentation navigation')}
              <ChevronDown aria-hidden='true' className='ml-auto size-4' />
            </CollapsibleTrigger>
            <CollapsibleContent>
              <nav
                className='space-y-7 pb-6'
                aria-label={t('Model API reference')}
              >
                {GROUPS.map((group) => (
                  <div key={group}>
                    <p className='text-muted-foreground mb-3 text-xs font-semibold'>
                      {t(group)}
                    </p>
                    <div className='flex flex-col border-l'>
                      {PAGES.filter((item) => item.group === group).map(
                        (item) => (
                          <Link
                            key={item.id}
                            to={
                              item.id === 'overview' ? '/docs' : '/docs/$page'
                            }
                            params={{ page: item.id }}
                            activeOptions={{ exact: true }}
                            aria-current={
                              pageId === item.id ? 'page' : undefined
                            }
                            className={cn(
                              '-ml-px border-l-2 px-4 py-2 text-sm transition-colors hover:text-foreground',
                              pageId === item.id
                                ? 'border-primary bg-primary/5 text-foreground font-medium'
                                : 'text-muted-foreground border-transparent'
                            )}
                            onClick={() => setMenuOpen(false)}
                          >
                            {t(item.title)}
                          </Link>
                        )
                      )}
                    </div>
                  </div>
                ))}
              </nav>
            </CollapsibleContent>
          </Collapsible>
        </aside>
        <main id='content' className='min-w-0 pb-16' key={pageId}>
          <header className='mb-8 space-y-4 border-b pb-7'>
            <p className='text-muted-foreground text-sm'>
              {t('Docs')}{' '}
              <span aria-hidden='true' className='px-2'>
                /
              </span>{' '}
              {t('Model API reference')}
            </p>
            <h1 className='text-3xl font-semibold tracking-tight'>
              {t(page.title)}
            </h1>
            <p className='text-muted-foreground text-base leading-7'>
              {doc
                ? t(doc.summary)
                : t(
                    'HTTP endpoints, authentication, request parameters, and response examples for calling models through aiscoo.'
                  )}
            </p>
          </header>
          {pageId === 'overview' && <ApiOverview />}
          {pageId === 'errors' && <ApiErrors />}
          {doc && <ApiEndpoint doc={doc} />}
          <nav
            className='mt-12 grid grid-cols-2 gap-4 border-t pt-6'
            aria-label={t('Documentation pages')}
          >
            {previous && (
              <Link
                to={previous.id === 'overview' ? '/docs' : '/docs/$page'}
                params={{ page: previous.id }}
                className='hover:border-primary/40 rounded-lg border p-4 transition-colors'
              >
                <span className='text-muted-foreground mb-2 flex items-center gap-2 text-xs'>
                  <ArrowLeft aria-hidden='true' className='size-3' />
                  {t('Previous page')}
                </span>
                <span className='text-sm font-medium'>{t(previous.title)}</span>
              </Link>
            )}
            {next && (
              <Link
                to='/docs/$page'
                params={{ page: next.id }}
                className='hover:border-primary/40 col-start-2 rounded-lg border p-4 text-right transition-colors'
              >
                <span className='text-muted-foreground mb-2 flex items-center justify-end gap-2 text-xs'>
                  {t('Next page')}
                  <ArrowRight aria-hidden='true' className='size-3' />
                </span>
                <span className='text-sm font-medium'>{t(next.title)}</span>
              </Link>
            )}
          </nav>
        </main>
        <aside
          className='sticky top-24 hidden self-start border-l pl-5 xl:block'
          aria-label={t('On this page')}
        >
          <p className='mb-4 text-xs font-semibold'>{t('On this page')}</p>
          <nav className='flex flex-col gap-3 text-xs'>
            {sections.map(([id, title]) => (
              <a
                key={id}
                href={`#${id}`}
                className='text-muted-foreground hover:text-foreground leading-5'
              >
                {t(title)}
              </a>
            ))}
          </nav>
        </aside>
      </div>
    </PublicLayout>
  )
}
