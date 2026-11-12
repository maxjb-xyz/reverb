/** The key that decides when a navigation counts as a new page: the pathname,
 *  except that Settings sections are one page (switching a section should not
 *  replay the whole page entrance or jump the scroll). */
export function pageKey(pathname: string): string {
  return pathname.startsWith('/settings') || pathname.startsWith('/admin') ? '/settings' : pathname
}
