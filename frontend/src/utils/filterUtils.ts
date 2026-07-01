export function fuzzyCaseInsensitive(filter: string | undefined, value: string): boolean {
  if (!filter) return true
  return value.toLowerCase().includes(filter.toLowerCase())
}
