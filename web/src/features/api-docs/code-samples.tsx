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

import { CodeBlock } from '@/components/ai-elements/code-block'
import { CopyButton } from '@/components/copy-button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import type { ApiCodeSample } from './reference'

interface CodeSamplesProps {
  samples: ApiCodeSample[]
}

export function CodeSamples(props: CodeSamplesProps) {
  const { t } = useTranslation()
  const origin = window.location.origin

  return (
    <Tabs defaultValue={props.samples[0]?.label ?? props.samples[0]?.language}>
      {props.samples.length > 1 && (
        <TabsList
          aria-label={t('Example language')}
          className='h-auto max-w-full flex-wrap justify-start'
        >
          {props.samples.map((sample) => {
            let label = sample.language === 'bash' ? 'cURL' : sample.language
            if (sample.label) label = t(sample.label)

            return (
              <TabsTrigger
                key={sample.label ?? sample.language}
                value={sample.label ?? sample.language}
              >
                {label}
              </TabsTrigger>
            )
          })}
        </TabsList>
      )}
      {props.samples.map((sample) => {
        const code = sample.code
          .replaceAll('__OPENAI_BASE__', `${origin}/v1`)
          .replaceAll('__API_ORIGIN__', origin)
        let title = sample.language === 'bash' ? 'cURL' : sample.language
        if (sample.label) title = t(sample.label)

        return (
          <TabsContent
            key={sample.label ?? sample.language}
            value={sample.label ?? sample.language}
          >
            <CodeBlock
              code={code}
              language={sample.language}
              title={title}
              showToolbar
              enableCollapse={false}
            >
              <CopyButton
                value={code}
                aria-label={t('Copy code')}
                className='size-8'
              />
            </CodeBlock>
          </TabsContent>
        )
      })}
    </Tabs>
  )
}
