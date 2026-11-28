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
import { Button } from '@/components/ui/button'
import { DataTableBulkActions } from './bulk-actions'

type RowItem = { id: string; name: string }

const columns: ColumnDef<RowItem>[] = [{ accessorKey: 'name', header: 'Name' }]
const data: RowItem[] = [
  { id: '1', name: 'Alpha' },
  { id: '2', name: 'Beta' },
]

function BulkActionsHarness({
  initialSelected = { '0': true },
  onReset = vi.fn(),
}: {
  initialSelected?: Record<string, boolean>
  onReset?: () => void
}) {
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    initialState: { rowSelection: initialSelected },
  })
  const origReset = table.resetRowSelection
  table.resetRowSelection = (defaultState?: boolean) => {
    onReset()
    origReset(defaultState)
  }

  return (
    <I18nextProvider i18n={i18n}>
      <DataTableBulkActions table={table} entityName='user'>
        <Button type='button' size='sm'>
          Export
        </Button>
        <Button type='button' size='sm' data-slot='dropdown-menu-trigger'>
          More
        </Button>
      </DataTableBulkActions>
    </I18nextProvider>
  )
}

describe('DataTableBulkActions', () => {
  it('announces current-page selection and clears selection on button or Escape', async () => {
    const onReset = vi.fn()
    const { getByRole, getByText } = await render(
      <BulkActionsHarness onReset={onReset} />
    )

    const toolbar = getByRole('toolbar')
    await expect.element(toolbar).toBeVisible()
    await expect
      .element(
        getByText(/当前页已选 1 个user|1 user selected on current page/i)
      )
      .toBeInTheDocument()

    const clearBtn = getByRole('button', {
      name: /clear selection|清除选择/i,
    })
    const exportBtn = getByRole('button', { name: 'Export' })
    const moreBtn = getByRole('button', { name: 'More' })

    clearBtn.element().focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(document.activeElement).toBe(exportBtn.element())

    await userEvent.keyboard('{End}')
    expect(document.activeElement).toBe(moreBtn.element())

    // Escape on dropdown-menu-trigger must not clear selection
    await userEvent.keyboard('{Escape}')
    expect(onReset).not.toHaveBeenCalled()

    await userEvent.keyboard('{Home}')
    expect(document.activeElement).toBe(clearBtn.element())

    await userEvent.keyboard('{Escape}')
    expect(onReset).toHaveBeenCalledOnce()
  })
})
