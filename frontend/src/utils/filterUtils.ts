export interface FilterValue {
  selected?: string[]
}

export interface RowSearchFilter<R> {
  filter: (input: FilterValue, obj: R) => boolean
  filterGroupName: string
  placeholder?: string
  type: string
}

export function fuzzyCaseInsensitive(filter: string | undefined, value: string): boolean {
  if (!filter) return true
  return value.toLowerCase().includes(filter.toLowerCase())
}
