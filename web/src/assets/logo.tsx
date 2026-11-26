import { useId, type SVGProps } from 'react'
import { cn } from '@/lib/utils'

type LogoProps = SVGProps<SVGSVGElement> & {
  /**
   * - `mono`: inherits currentColor for small UI surfaces (sidebar, buttons, glass badges).
   * - `brand`: uses the aurora gradient palette for standalone brand showcases.
   */
  variant?: 'mono' | 'brand'
}

/**
 * Tianshu (天枢) brand mark: 270° armillary arc, central T-pivot, and Dubhe polar beacon.
 */
export function Logo({ variant = 'mono', className, ...props }: LogoProps) {
  const gradientId = useId()
  const isBrand = variant === 'brand'

  return (
    <svg
      viewBox='0 0 24 24'
      xmlns='http://www.w3.org/2000/svg'
      height='24'
      width='24'
      fill='none'
      stroke={isBrand ? `url(#${gradientId})` : 'currentColor'}
      strokeWidth='2.2'
      strokeLinecap='round'
      strokeLinejoin='round'
      aria-hidden='true'
      className={cn('size-6 shrink-0', className)}
      {...props}
    >
      {isBrand && (
        <defs>
          <linearGradient
            id={gradientId}
            x1='3'
            y1='21'
            x2='21'
            y2='3'
            gradientUnits='userSpaceOnUse'
          >
            <stop offset='0%' stopColor='#93c5fd' />
            <stop offset='55%' stopColor='#e0e7ff' />
            <stop offset='100%' stopColor='#67e8f9' />
          </linearGradient>
        </defs>
      )}
      {/* 270° celestial armillary arc open toward the northeast polar bearing */}
      <path d='M20.25 12A8.25 8.25 0 1 1 12 3.75' />
      {/* Central T-pivot representing Tianshu and the core system hub */}
      <path d='M8.5 9.25H15.5M12 9.25V16.25' />
      {/* Dubhe (天枢) polar beacon */}
      <circle
        cx='17.85'
        cy='6.15'
        r='2.1'
        fill={isBrand ? '#67e8f9' : 'currentColor'}
        stroke='none'
      />
    </svg>
  )
}
