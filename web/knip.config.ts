import type { KnipConfig } from 'knip'

const config: KnipConfig = {
  // Base scaffold retains shadcn/ui primitives, data-table kit, and contract types for upcoming modules.
  ignore: [
    'src/components/ui/**',
    'src/components/data-table/**',
    'src/components/shared/**',
    'src/lib/api/types.ts',
    'src/tanstack-table.d.ts',
  ],
  ignoreDependencies: [
    '@radix-ui/*',
    'date-fns',
    'react-day-picker',
    'recharts',
  ],
  ignoreExportsUsedInFile: true,
}

export default config
