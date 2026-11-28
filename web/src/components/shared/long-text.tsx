import { useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

type LongTextProps = {
  children: React.ReactNode
  className?: string
  contentClassName?: string
}

export function LongText({
  children,
  className = '',
  contentClassName = '',
}: LongTextProps) {
  const singleRef = useRef<HTMLDivElement>(null)
  const desktopRef = useRef<HTMLDivElement>(null)
  const mobileRef = useRef<HTMLDivElement>(null)
  const [isOverflown, setIsOverflown] = useState(false)

  useEffect(() => {
    const measure = () => {
      const candidates = [
        singleRef.current,
        desktopRef.current,
        mobileRef.current,
      ].filter(
        (node): node is HTMLDivElement =>
          node !== null && node.getClientRects().length > 0
      )
      if (candidates.length === 0) return
      const next = candidates.some((node) => checkOverflow(node))
      setIsOverflown((prev) => (prev === next ? prev : next))
    }

    measure()

    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    if (singleRef.current) observer.observe(singleRef.current)
    if (desktopRef.current) observer.observe(desktopRef.current)
    if (mobileRef.current) observer.observe(mobileRef.current)
    return () => observer.disconnect()
  }, [children, isOverflown])

  if (!isOverflown)
    return (
      <div ref={singleRef} className={cn('truncate', className)}>
        {children}
      </div>
    )

  return (
    <>
      <div className='hidden sm:block'>
        <Tooltip>
          <TooltipTrigger asChild>
            <div
              ref={desktopRef}
              tabIndex={0}
              className={cn(
                'truncate focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none',
                className
              )}
            >
              {children}
            </div>
          </TooltipTrigger>
          <TooltipContent>
            <p className={contentClassName}>{children}</p>
          </TooltipContent>
        </Tooltip>
      </div>
      <div className='sm:hidden'>
        <Popover>
          <PopoverTrigger asChild>
            <div ref={mobileRef} className={cn('truncate', className)}>
              {children}
            </div>
          </PopoverTrigger>
          <PopoverContent className={cn('w-fit', contentClassName)}>
            <p>{children}</p>
          </PopoverContent>
        </Popover>
      </div>
    </>
  )
}

const checkOverflow = (textContainer: HTMLDivElement | null) => {
  if (textContainer) {
    return (
      textContainer.offsetHeight < textContainer.scrollHeight ||
      textContainer.offsetWidth < textContainer.scrollWidth
    )
  }
  return false
}
