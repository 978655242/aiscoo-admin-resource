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
import { Zap, Shield, Globe, Code } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

import { GlowCard } from '../glow-card'

interface FeaturesProps {
  className?: string
}

const CARD_BG = [
  'linear-gradient(150deg, #1b6f86, #0e3a4d 72%)',
  'linear-gradient(150deg, #3a6ed8, #1b3473 72%)',
  'linear-gradient(150deg, #8b5cf6, #3b2a85 72%)',
  'linear-gradient(150deg, #e2804c, #8a3a25 72%)',
]

export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()

  const features = [
    {
      id: 'fast',
      title: t('Lightning Fast'),
      desc: t(
        'Optimized network architecture ensures millisecond response times'
      ),
      icon: <Zap className='size-5' />,
      chips: ['OpenAI', 'Claude', 'Gemini', 'DeepSeek'],
    },
    {
      id: 'secure',
      title: t('Secure & Reliable'),
      desc: t(
        'Enterprise-grade security with comprehensive permission management'
      ),
      icon: <Shield className='size-5' />,
      chips: [t('Load Balancing'), t('Rate Limiting')],
    },
    {
      id: 'global',
      title: t('Global Coverage'),
      desc: t('Multi-region deployment for stable global access'),
      icon: <Globe className='size-5' />,
      chips: [t('Cost Tracking'), t('Load Balancing')],
    },
    {
      id: 'developer',
      title: t('Developer Friendly'),
      desc: t('Compatible API routes for common AI application workflows'),
      icon: <Code className='size-5' />,
      chips: ['API', 'SDK', 'CLI', 'Docs'],
    },
  ]

  return (
    <section className='relative z-10 px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-14 max-w-xl'>
          <p className='mb-3 text-xs font-medium tracking-widest text-[#357581] uppercase'>
            {t('Core Features')}
          </p>
          <h2 className='text-[clamp(2rem,3.5vw,3.25rem)] leading-[1.25] font-normal tracking-[-0.045em] text-balance text-[#153640]'>
            {t('Built for developers,')}
            <br />
            {t('designed for scale')}
          </h2>
        </AnimateInView>

        <div className='grid gap-5 sm:grid-cols-2 lg:grid-cols-4'>
          {features.map((feature, i) => (
            <AnimateInView key={feature.id} delay={i * 120} animation='fade-up'>
              <GlowCard
                className='h-full min-h-[260px] rounded-2xl border border-white/20 p-5 text-[#f4fbfb] shadow-[0_14px_30px_rgba(12,40,48,0.22),inset_0_1px_0_rgba(255,255,255,0.22)]'
                style={{ background: CARD_BG[i] }}
              >
                <div className='pointer-events-none absolute -top-16 -right-12 size-44 rounded-full bg-[radial-gradient(circle,rgba(255,255,255,0.2),transparent_68%)]' />
                <div className='relative z-[2] flex h-full flex-col'>
                  <span className='text-[11px] tracking-[0.08em] opacity-70'>
                    {String(i + 1).padStart(2, '0')}
                  </span>
                  <span className='mt-4 grid size-10 place-items-center rounded-[11px] bg-white/[0.16]'>
                    {feature.icon}
                  </span>
                  <h3 className='mt-5 text-base font-semibold'>
                    {feature.title}
                  </h3>
                  <p className='mt-1.5 text-[13px] leading-relaxed opacity-85'>
                    {feature.desc}
                  </p>
                  <div className='mt-auto flex flex-wrap gap-1.5 pt-5'>
                    {feature.chips.map((chip) => (
                      <span
                        key={chip}
                        className='rounded-md border border-white/20 bg-white/10 px-2 py-0.5 text-[11px] text-white/90'
                      >
                        {chip}
                      </span>
                    ))}
                  </div>
                </div>
              </GlowCard>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
