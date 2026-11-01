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
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { Button } from '@/components/ui/button'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

export function CTA(props: CTAProps) {
  const { t } = useTranslation()

  if (props.isAuthenticated) {
    return null
  }

  return (
    <section className='dark relative isolate z-10 overflow-hidden bg-[#09131f] px-6 py-24 md:py-32'>
      <div
        aria-hidden
        className='pointer-events-none absolute inset-x-[-1px] bottom-0 -z-10 h-[70%]'
        style={{
          backgroundImage: 'url(/brand/horizon.png)',
          backgroundSize: '100% 100%',
          maskImage: 'linear-gradient(transparent, #000 40px)',
          WebkitMaskImage: 'linear-gradient(transparent, #000 40px)',
        }}
      />
      <div
        aria-hidden
        className='hero-orbital pointer-events-none absolute top-[70%] left-1/2 -z-10 h-[430px] w-[1400px] -translate-x-1/2 -translate-y-1/2 rounded-full border border-[rgba(125,227,234,0.08)]'
      />

      <AnimateInView
        className='mx-auto max-w-2xl text-center'
        animation='scale-in'
      >
        <h2 className='text-[clamp(2rem,4vw,3.75rem)] leading-[1.25] font-normal tracking-[-0.045em] text-[#f5f8fa]'>
          {t('Ready to simplify')}
          <br />
          <span className='bg-gradient-to-r from-[#d5f7f6] via-[#46dcec] to-[#8ddbe2] bg-clip-text text-transparent'>
            {t('your AI integration?')}
          </span>
        </h2>
        <p className='mx-auto mt-5 max-w-md text-sm leading-[1.9] text-[#a0b4c2] md:text-base'>
          {t(
            'Deploy your own gateway and start routing requests through your configured upstream services.'
          )}
        </p>
        <div className='mt-8 flex items-center justify-center gap-3'>
          <Button
            className='group h-[46px] rounded-[7px] border border-[#f5f0e5] bg-[#f5f0e5] px-6 text-sm font-medium text-[#202c32] transition-all hover:-translate-y-0.5 hover:bg-[#fffaf0]'
            render={<Link to='/sign-up' />}
          >
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
        </div>
      </AnimateInView>
    </section>
  )
}
