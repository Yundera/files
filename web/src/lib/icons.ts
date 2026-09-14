// File-type icons, ported from CasaOS-UI's mixins/mixin.js `typeMap` and
// `getIconFile`. The extension lists are verbatim — this is the mapping users
// recognise, and a "tidied" version would silently change which icon a file gets.
//
// Only the ~35 icons this map can actually reach were copied out of
// casa-img/CasaOS-UI/src/assets/img/filebrowser/; the other ~190 there are
// unreachable from the map and are not worth carrying.
import type { Entry } from './types'

const urls = import.meta.glob('./icons/*.svg', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

function url(name: string): string {
  return urls[`./icons/${name}.svg`] ?? urls['./icons/unknown.svg']
}

// Order matters only in that a later match wins, matching CasaOS's forEach:
// 'dockerfile' appears in both text-x-cmake and text-dockerfile, and the latter
// is what CasaOS ends up showing.
const typeMap: Array<[string, string[]]> = [
  ['image-x-generic', ['png', 'jpg', 'jpeg', 'bmp', 'gif', 'webp', 'svg', 'tiff']],
  ['video-x-generic', ['mkv', 'mp4', '3gp', 'avi', 'm2ts', 'webm', 'flv', 'vob', 'ts', 'mts', 'mov', 'wmv', 'rm', 'rmvb', 'asf', 'mpg', 'm4v', 'mpeg', 'f4v']],
  ['audio-x-generic', ['aac', 'aiff', 'alac', 'amr', 'ape', 'flac', 'm4a', 'mp3', 'ogg', 'opus', 'wma', 'wav']],
  ['text-x-generic', ['txt', 'log', 'pages', 'conf', 'config', 'list', 'ini', 'toml', 'cfg', 'rc', 'env', 'service', 'htaccess', 'gitconfig', 'vim', 'curlrc', 'wgetrc', 'gitignore']],
  ['text-markdown', ['md']],
  ['text-css', ['php', 'css', 'less', 'scss', 'sass', 'aspx', 'lua', 'vue', 'js', 'go', 'asp', 'bat', 'c', 'cpp', 'cs', 'json', 'py', 'perl', 'sh', 'xml', 'yaml', 'vb', 'vbs', 'sql', 'swift', 'rust', 'rs', 'jsp', 'yml', 'r', 'pl', 'rb', 'src', 'h', 'tex', 'rtf', 'jsonld', 'ttl', 'n3', 'rss', 'atom', 'srt', 'ass', 'tsv', 'vcard', 'asc', 'url', 'diff', 'plaintext']],
  ['text-html', ['html', 'htm', 'shtml', 'shtm']],
  ['application-vnd.ms-word', ['doc', 'docx', 'wps']],
  ['application-vnd.ms-excel', ['xls', 'xlsx', 'csv']],
  ['application-vnd.ms-powerpoint', ['ppt', 'pptx']],
  ['application-pdf', ['pdf']],
  ['application-photoshop', ['psd', 'psb']],
  ['application-illustrator', ['ai', 'eps']],
  ['application-x-wine-extension-cpl', ['exe']],
  ['application-apk', ['apk']],
  ['application-x-zip', ['zip', 'rar', '7z', 'gz', 'ace', 'xz']],
  ['application-x-cd-image', ['iso', 'img', 'vmdk', 'raw', 'vhd']],
  ['application-x-apple', ['dmg', 'ipa', 'pkg']],
  ['application-x-pem-key', ['pem', 'crt', 'ca-bundle', 'p7b', 'p7s', 'der', 'cer', 'pfx', 'p12']],
  ['text-x-cmake', ['makefile', 'cmake', 'dockerfile']],
  ['text-dockerfile', ['dockerfile']],
]

// Named folders get their own icon. These are the CasaOS names; a PCS has
// exactly these directories under /DATA.
const folderByName: Record<string, string> = {
  Media: 'folder-video',
  Downloads: 'folder-download',
  Documents: 'folder-documents',
  Gallery: 'folder-pictures',
  AppData: 'folder-application',
}

export function iconFor(entry: Entry): string {
  if (entry.kind === 'dir') return url(folderByName[entry.name] ?? 'folder-default')
  const ext = (entry.ext ?? '').toLowerCase()
  // CasaOS matches on the whole lowercased filename for extensionless build
  // files, which is how "Dockerfile" and "Makefile" get an icon at all.
  const key = ext || entry.name.toLowerCase()
  let icon = 'unknown'
  for (const [name, exts] of typeMap) if (exts.includes(key)) icon = name
  return url(icon)
}

export const rootIcon = (name: string) => url(folderByName[name] ?? 'folder-default')
export const illustration = (name: 'newFile' | 'newFolder' | 'upfile' | 'upfolder') => url(name)
