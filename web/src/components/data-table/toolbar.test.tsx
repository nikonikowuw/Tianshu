import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n from '@/i18n/index'
import { DataTableToolbar } from './toolbar'

type RowItem = { id: string; name: string }
const columns: ColumnDef<RowItem>[] = [{ accessorKey: 'name', header: 'Name' }]

function ToolbarHarness({
  externalFilter,
  onGlobalFilterChange,
  debounceMs = 60,
}: {
  externalFilter: string
  onGlobalFilterChange: (next: string) => void
  debounceMs?: number
}) {
  const table = useReactTable({
    data: [],
    columns,
    state: { globalFilter: externalFilter },
    onGlobalFilterChange: (updater) => {
      const next =
        typeof updater === 'function' ? updater(externalFilter) : updater
      onGlobalFilterChange(next)
    },
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <I18nextProvider i18n={i18n}>
      <DataTableToolbar table={table} debounceMs={debounceMs} />
    </I18nextProvider>
  )
}

describe('DataTableToolbar', () => {
  it('cancels pending debounce timer when external filter changes before debounce expires', async () => {
    const onFilterChange = vi.fn()

    function StatefulParent() {
      const [filter, setFilter] = useState('initial')
      return (
        <>
          <button type='button' onClick={() => setFilter('restored')}>
            Simulate Back
          </button>
          <ToolbarHarness
            externalFilter={filter}
            debounceMs={120}
            onGlobalFilterChange={(val) => {
              onFilterChange(val)
              setFilter(val)
            }}
          />
        </>
      )
    }

    const clearTimeoutSpy = vi.spyOn(globalThis, 'clearTimeout')
    const { getByRole } = await render(<StatefulParent />)
    const input = getByRole('textbox')

    await userEvent.clear(input)
    await userEvent.type(input, 'stale-query')
    const clearsBeforeNav = clearTimeoutSpy.mock.calls.length
    // External navigation occurs before 120ms debounce expires
    await userEvent.click(getByRole('button', { name: 'Simulate Back' }))

    await expect.element(input).toHaveValue('restored')
    expect(clearTimeoutSpy.mock.calls.length).toBeGreaterThan(clearsBeforeNav)
    expect(onFilterChange).not.toHaveBeenCalledWith('stale-query')
    clearTimeoutSpy.mockRestore()
  })

  it('normalizes whitespace-only input to empty string', async () => {
    const onFilterChange = vi.fn()
    const { getByRole } = await render(
      <ToolbarHarness
        externalFilter='alice'
        debounceMs={60}
        onGlobalFilterChange={onFilterChange}
      />
    )

    const input = getByRole('textbox')
    await userEvent.clear(input)
    await userEvent.type(input, '   ')

    await vi.waitFor(() => {
      expect(onFilterChange).toHaveBeenLastCalledWith('')
    })
  })
})
