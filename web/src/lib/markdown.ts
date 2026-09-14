// Markdown rendering for the preview pane.
//
// DOMPurify is not optional here. A .md file in the user's tree is untrusted
// content that can contain raw HTML, and the preview renders on this app's own
// origin — which sits behind a shared SSO gate, so script running here runs
// inside the user's authenticated session.
import { marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({ gfm: true, breaks: false })

export function renderMarkdown(src: string): string {
  const raw = marked.parse(src, { async: false }) as string
  return DOMPurify.sanitize(raw, {
    // Block javascript: and data: URLs in links and images. DOMPurify allows
    // data: on images by default, which is a vector for SVG-borne script.
    ALLOWED_URI_REGEXP: /^(?:https?|mailto|tel|#|\/)/i,
    ADD_ATTR: ['target', 'rel'],
  })
}
