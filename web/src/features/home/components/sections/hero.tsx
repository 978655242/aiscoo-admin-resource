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
import { ClaudeCode, Codex } from '@lobehub/icons'
import { Link } from '@tanstack/react-router'
import { ArrowRight, BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import openCodeLogo from '@/assets/brand-icons/opencode.svg'
import { Button } from '@/components/ui/button'

import { HeroEqualizer } from '../hero-equalizer'
import { HeroStage } from '../hero-stage'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const docsButton = (
    <Button
      role='link'
      variant='outline'
      className='group inline-flex h-[46px] items-center gap-2 rounded-[7px] border-[rgba(180,221,231,0.23)] bg-white/[0.03] px-6 text-sm font-medium text-[#dbe8ed] transition-all hover:-translate-y-0.5 hover:bg-[rgba(180,221,231,0.09)] hover:text-white'
      render={<Link to='/docs' />}
    >
      <BookOpen className='size-4' />
      <span>{t('Docs')}</span>
    </Button>
  )

  const primaryCta =
    'group h-[46px] rounded-[7px] border border-[#f5f0e5] bg-[#f5f0e5] px-6 text-sm font-medium text-[#202c32] shadow-[0_3px_15px_rgba(0,0,0,0.08)] transition-all hover:-translate-y-0.5 hover:bg-[#fffaf0]'

  return (
    <section className='dark text-foreground relative isolate z-10 overflow-hidden bg-[#09131f] pt-24 md:pt-28'>
      {/* Official-site sky: night blue fading into the cyan horizon line */}
      <div aria-hidden className='pointer-events-none absolute inset-0 -z-30'>
        <div
          className='absolute inset-x-0 top-0 h-[49%]'
          style={{
            backgroundImage: 'url(/brand/horizon.png)',
            backgroundSize: '100% 550%',
            backgroundPosition: 'center top',
          }}
        />
        <div className='absolute inset-x-0 top-[25%] bottom-[21%] bg-[#09131f]' />
        <div
          className='absolute inset-x-[-1px] bottom-0 h-[40%] min-h-[300px]'
          style={{
            backgroundImage: 'url(/brand/horizon.png)',
            backgroundSize: '100% 100%',
            maskImage: 'linear-gradient(transparent, #000 40px)',
            WebkitMaskImage: 'linear-gradient(transparent, #000 40px)',
          }}
        />
        <div className='absolute inset-0 [background:radial-gradient(ellipse_at_50%_72%,rgba(22,170,191,0.13),transparent_49%)]' />
      </div>
      {/* Orbital rings */}
      <div
        aria-hidden
        className='hero-orbital pointer-events-none absolute top-[62%] left-1/2 -z-20 h-[430px] w-[1400px] -translate-x-1/2 -translate-y-1/2 rounded-full border border-[rgba(125,227,234,0.08)]'
      >
        <div className='absolute inset-x-[100px] inset-y-[45px] rounded-full border border-[rgba(125,227,234,0.05)]' />
      </div>
      <HeroEqualizer className='pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-[62%] w-full opacity-80' />

      <div className='relative mx-auto max-w-[1120px] px-5 text-center'>
        <div
          className='landing-animate-fade-up inline-flex items-center gap-2.5 text-[13px] tracking-[0.05em] text-[#bfd0da] opacity-0'
          style={{ animationDelay: '0ms' }}
        >
          <span className='size-[5px] rounded-full bg-[#89e6e9] shadow-[0_0_12px_rgba(70,220,236,0.6)]' />
          {t('AI Application Infrastructure Foundation')}
        </div>

        <h1
          className='landing-animate-fade-up mt-6 mb-5 text-[clamp(2.5rem,5.35vw,5.5rem)] leading-[1.2] font-normal tracking-[-0.055em] text-balance text-[#f5f8fa]'
          style={{ animationDelay: '100ms' }}
        >
          {t('Unified API Gateway for')}
          <span className='block bg-gradient-to-r from-[#d5f7f6] via-[#46dcec] to-[#8ddbe2] bg-clip-text text-transparent'>
            {t('Vast Range of AI Models')}
          </span>
        </h1>
        <p
          className='landing-animate-fade-up mx-auto max-w-2xl text-[15px] leading-[1.9] tracking-[0.02em] text-[#a0b4c2] opacity-0'
          style={{ animationDelay: '180ms' }}
        >
          {t(
            'Access a vast selection of models via a standard, unified API protocol. Power AI applications, manage digital assets, and connect the Future.'
          )}
        </p>

        <div
          className='landing-animate-fade-up mt-7 flex flex-wrap items-center justify-center gap-3 opacity-0'
          style={{ animationDelay: '260ms' }}
        >
          {props.isAuthenticated ? (
            <>
              <Button className={primaryCta} render={<Link to='/dashboard' />}>
                {t('Go to Dashboard')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
              {docsButton}
            </>
          ) : (
            <>
              <Button className={primaryCta} render={<Link to='/sign-up' />}>
                {t('Get Started')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
              <Button
                variant='outline'
                className='h-[46px] rounded-[7px] border-[rgba(180,221,231,0.23)] bg-white/[0.03] px-6 text-sm font-medium text-[#dbe8ed] transition-all hover:-translate-y-0.5 hover:bg-[rgba(180,221,231,0.09)] hover:text-white'
                render={<Link to='/pricing' />}
              >
                {t('View Pricing')}
              </Button>
              {docsButton}
            </>
          )}
        </div>

        <div
          className='landing-animate-fade-up mt-8 flex flex-wrap items-center justify-center gap-2.5 opacity-0'
          style={{ animationDelay: '340ms' }}
        >
          <span className='mr-1 text-[11px] tracking-[0.12em] text-[#849aa9] uppercase'>
            {t('Supported Applications')}
          </span>
          <a
            href='https://claude.com/product/claude-code'
            target='_blank'
            rel='noopener noreferrer'
            className='group flex items-center gap-2.5 rounded-full border border-[rgba(204,238,244,0.17)] bg-[rgba(221,239,243,0.045)] px-4 py-2 text-sm text-[#d4dce2] transition-colors hover:bg-[rgba(221,239,243,0.13)] hover:text-white'
          >
            <ClaudeCode.Color size={20} aria-hidden='true' />
            <span>Claude Code</span>
          </a>
          <a
            href='https://openai.com/codex/'
            target='_blank'
            rel='noopener noreferrer'
            className='group flex items-center gap-2.5 rounded-full border border-[rgba(204,238,244,0.17)] bg-[rgba(221,239,243,0.045)] px-4 py-2 text-sm text-[#d4dce2] transition-colors hover:bg-[rgba(221,239,243,0.13)] hover:text-white'
          >
            <Codex size={20} aria-hidden='true' />
            <span>Codex</span>
          </a>
          <a
            href='https://opencode.ai/'
            target='_blank'
            rel='noopener noreferrer'
            className='group flex items-center gap-2.5 rounded-full border border-[rgba(204,238,244,0.17)] bg-[rgba(221,239,243,0.045)] px-4 py-2 text-sm text-[#d4dce2] transition-colors hover:bg-[rgba(221,239,243,0.13)] hover:text-white'
          >
            <img
              src={openCodeLogo}
              alt=''
              className='size-5 shrink-0 object-contain'
            />
            <span>Open Code</span>
          </a>
        </div>
      </div>

      <HeroStage />
    </section>
  )
}
