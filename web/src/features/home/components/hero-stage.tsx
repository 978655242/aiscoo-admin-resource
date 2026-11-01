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

import { HeroTerminalDemo } from './hero-terminal-demo'

const PROVIDERS = ['OpenAI', 'Anthropic', 'Gemini']

/**
 * Product stage under the hero copy: the API console rising from the horizon
 * with two tilted glass cards beside it, as on the official site.
 */
export function HeroStage() {
  const { t } = useTranslation()

  return (
    <div className='relative mx-auto mt-14 h-[360px] w-full overflow-hidden sm:h-[420px]'>
      <div
        className='landing-animate-fade-up absolute top-0 left-1/2 w-[min(92vw,720px)] -translate-x-1/2 opacity-0'
        style={{ animationDelay: '250ms', animationDuration: '1.2s' }}
      >
        <HeroTerminalDemo className='max-w-none' />
      </div>

      <div
        className='landing-animate-fade-up absolute top-16 hidden opacity-0 xl:block'
        style={{ left: 'calc(50% - 560px)', animationDelay: '600ms' }}
      >
        <div className='hero-float w-[217px] -rotate-[4deg] rounded-[11px] border border-white/50 bg-[rgba(205,244,244,0.88)] p-4 text-[#193d49] shadow-[0_14px_45px_rgba(4,40,47,0.25)] backdrop-blur-xl'>
          <div className='flex items-center gap-2.5 text-sm font-semibold'>
            <span className='grid size-7 place-items-center rounded-[7px] bg-[#133b47] p-[3px]'>
              <img src='/brand/mark-dark.svg' alt='' className='size-full' />
            </span>
            aiscoo
          </div>
          {PROVIDERS.map((name) => (
            <div
              key={name}
              className='flex items-center justify-between pt-3 text-xs text-[#51707a]'
            >
              <span>{name}</span>
              <span className='flex items-center gap-1.5 text-[#23645b]'>
                <span className='size-1 rounded-full bg-[#469a88]' />
                {t('Online')}
              </span>
            </div>
          ))}
        </div>
      </div>

      <div
        className='landing-animate-fade-up absolute top-28 hidden opacity-0 xl:block'
        style={{ right: 'calc(50% - 574px)', animationDelay: '700ms' }}
      >
        <div
          className='hero-float w-[247px] rotate-[4deg] rounded-[11px] border border-white/50 bg-[#e4efeb] p-[15px] text-[#193d49] shadow-[0_14px_45px_rgba(4,40,47,0.25)]'
          style={{
            backgroundImage:
              'radial-gradient(#a5bebb 0.55px, transparent 0.55px)',
            backgroundSize: '5px 5px',
            animationDelay: '-3s',
          }}
        >
          <div className='mb-3.5 font-mono text-[10px] text-[#587277]'>
            POST /v1/chat/completions
          </div>
          <div className='rounded-[7px_7px_2px_7px] bg-[#153641] p-3.5 font-mono text-xs leading-[1.8] text-[#def4f3]'>
            <div>&quot;model&quot;: &quot;gpt-4o&quot;,</div>
            <div>&quot;stream&quot;: true</div>
          </div>
          <div className='mt-3.5 flex items-center justify-between gap-3 pl-0.5'>
            <span className='text-[10px] text-[#647e82]'>
              {t('Latency')} 128 ms
            </span>
            <span className='hero-pulse grid size-8 place-items-center rounded-full border border-[#37717a] bg-[#143b46]'>
              <span className='size-2 rounded-full bg-[#bffbfa]' />
            </span>
          </div>
        </div>
      </div>

      <div className='pointer-events-none absolute inset-x-0 bottom-0 h-14 bg-gradient-to-b from-transparent to-[#eff9f7]' />
    </div>
  )
}
