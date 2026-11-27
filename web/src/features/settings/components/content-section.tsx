type ContentSectionProps = {
  title?: string
  desc?: string
  children: React.ReactNode
}

export function ContentSection({ title, desc, children }: ContentSectionProps) {
  return (
    <div className='space-y-6'>
      {(title || desc) && (
        <div className='space-y-1'>
          {title && (
            <h2 className='text-base font-semibold tracking-tight'>{title}</h2>
          )}
          {desc && <p className='text-sm text-muted-foreground'>{desc}</p>}
        </div>
      )}
      {children}
    </div>
  )
}
