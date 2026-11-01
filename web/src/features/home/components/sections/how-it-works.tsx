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
import { Settings, Zap, BarChart3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

import { GlowCard } from '../glow-card'

export function HowItWorks() {
  const { t } = useTranslation()

  const steps = [
    {
      num: '01',
      title: t('Configure'),
      desc: t(
        'Add your API keys, set up channels and configure access permissions'
      ),
      icon: <Settings className='size-6' strokeWidth={1.5} />,
    },
    {
      num: '02',
      title: t('Connect'),
      desc: t(
        'Connect through OpenAI, Claude, Gemini, and other compatible API routes'
      ),
      icon: <Zap className='size-6' strokeWidth={1.5} />,
    },
    {
      num: '03',
      title: t('Monitor'),
      desc: t('Track usage, costs and performance with real-time analytics'),
      icon: <BarChart3 className='size-6' strokeWidth={1.5} />,
    },
  ]

  return (
    <section className='relative z-10 bg-[#d5efed] px-6 py-24 text-[#143641] md:py-32'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-16 text-center md:mb-20'>
          <p className='mb-3 text-xs font-medium tracking-widest text-[#357581] uppercase'>
            {t('How It Works')}
          </p>
          <h2 className='text-[clamp(2rem,3.5vw,3.25rem)] leading-[1.25] font-normal tracking-[-0.045em] text-balance'>
            {t('Three steps to get started')}
          </h2>
        </AnimateInView>

        <div className='relative grid gap-6 md:grid-cols-3 md:gap-10'>
          <div
            aria-hidden
            className='flow-line absolute top-[3.25rem] right-[16.6%] left-[16.6%] hidden h-0.5 md:block'
          />
          {steps.map((step, i) => (
            <AnimateInView key={step.num} delay={i * 150} animation='fade-up'>
              <GlowCard className='h-full rounded-[9px] border border-[#a9cdcc] bg-[#ecf8f5] p-6 text-center'>
                <div className='relative z-[2] mx-auto mb-5 grid size-[52px] place-items-center rounded-full border border-[#37717a] bg-[#143b46] text-[#bffbfa] shadow-[0_0_0_6px_rgba(81,207,213,0.14),0_0_22px_rgba(38,176,187,0.25)]'>
                  {step.icon}
                </div>
                <span className='relative z-[2] text-[11px] tracking-[0.08em] text-[#357581]'>
                  {step.num}
                </span>
                <h3 className='relative z-[2] mt-1 mb-2 text-lg font-medium'>
                  {step.title}
                </h3>
                <p className='relative z-[2] mx-auto max-w-[240px] text-sm leading-relaxed text-[#466c76]'>
                  {step.desc}
                </p>
              </GlowCard>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
