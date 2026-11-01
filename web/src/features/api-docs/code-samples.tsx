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

interface CodeSamplesProps {
  samples: { language: string; code: string }[]
}

export function CodeSamples(props: CodeSamplesProps) {
  const { t } = useTranslation()
  const origin = window.location.origin

  return (
    <Tabs defaultValue={props.samples[0]?.language}>
      {props.samples.length > 1 && (
        <TabsList aria-label={t('Example language')}>
          {props.samples.map((sample) => (
            <TabsTrigger key={sample.language} value={sample.language}>
              {sample.language === 'bash' ? 'cURL' : sample.language}
            </TabsTrigger>
          ))}
        </TabsList>
      )}
      {props.samples.map((sample) => {
        const code = sample.code
          .replaceAll('__OPENAI_BASE__', `${origin}/v1`)
          .replaceAll('__API_ORIGIN__', origin)

        return (
          <TabsContent key={sample.language} value={sample.language}>
            <CodeBlock
              code={code}
              language={sample.language}
              title={sample.language === 'bash' ? 'cURL' : sample.language}
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
