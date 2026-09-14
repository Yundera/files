export type Kind = 'dir' | 'file' | 'symlink'

export interface Entry {
  name: string
  path: string
  kind: Kind
  size: number
  modTime: string
  mode: string
  ext?: string
  thumb?: boolean
}

export interface Page {
  path: string
  entries: Entry[]
  cursor?: string
  total: number
  truncated?: boolean
}

export type SortKey = 'name' | 'size' | 'modified' | 'kind'
export type ViewMode = 'grid' | 'list'

export type Policy = 'skip' | 'overwrite' | 'keepBoth'
