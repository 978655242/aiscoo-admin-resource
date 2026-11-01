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
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

type PageHeroProps = {
  title: ReactNode
  /** Short line directly under the title. */
  subtitle?: ReactNode
  /** Longer supporting text, muted. */
  description?: ReactNode
  /** Controls under the text, e.g. a search box or tabs. */
  children?: ReactNode
  className?: string
}

/**
 * Page header for the public pages (model square, rankings). Sits on the
 * regular mint canvas: only the top navigation bar is dark.
 */
export function PageHero(props: PageHeroProps) {
  return (
    <section
      className={cn('px-5 pt-[6.25rem] pb-4 text-center', props.className)}
    >
      <div className='landing-animate-fade-up mx-auto max-w-3xl'>
        <h1 className='text-foreground text-[clamp(2.25rem,5vw,4rem)] leading-[1.2] font-normal tracking-[-0.055em] text-balance'>
          {props.title}
        </h1>
        {props.subtitle != null && (
          <p className='mt-1.5 text-base text-[#14414b]'>{props.subtitle}</p>
        )}
        {props.description != null && (
          <p className='text-muted-foreground mx-auto mt-1 max-w-2xl text-sm leading-[1.9] tracking-[0.02em]'>
            {props.description}
          </p>
        )}
        {props.children != null && (
          <div className='mt-4 text-left'>{props.children}</div>
        )}
      </div>
    </section>
  )
}
