import { type SVGProps } from 'react'
import { cn } from '@/lib/utils'

export function Logo({ className, ...props }: SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox='0 0 24 24'
      xmlns='http://www.w3.org/2000/svg'
      height='24'
      width='24'
      fill='none'
      stroke='currentColor'
      strokeWidth='1.75'
      strokeLinecap='round'
      strokeLinejoin='round'
      className={cn('size-6', className)}
      {...props}
    >
      <title>Tianshu</title>
      {/* Open celestial orbit arc */}
      <path d='M20.34 8.63A9 9 0 1 1 15.37 3.66' />
      {/* Dubhe (天枢) polar beacon */}
      <circle cx='18.36' cy='5.64' r='1.6' fill='currentColor' stroke='none' />
      {/* Central compass pivot star */}
      <path
        d='M12 6.75L13.25 10.75L17.25 12L13.25 13.25L12 17.25L10.75 13.25L6.75 12L10.75 10.75Z'
        fill='currentColor'
        stroke='none'
      />
    </svg>
  )
}
