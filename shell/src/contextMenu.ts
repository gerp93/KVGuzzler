// The right-click menu. With the Edit menu gone (menu.ts), this is where cut, copy, paste and select all stay reachable.
import type { MenuItemConstructorOptions } from 'electron';

export interface ContextParams {
  isEditable: boolean;
  selectionText: string;
  editFlags: { canCut: boolean; canCopy: boolean; canPaste: boolean; canSelectAll: boolean };
}

/** The items for a right-click: only the actions that are really available where it was clicked. Empty when there are none. */
export function buildContextMenuTemplate(p: ContextParams): MenuItemConstructorOptions[] {
  const items: MenuItemConstructorOptions[] = [];
  if (p.isEditable) {
    if (p.editFlags.canCut) items.push({ role: 'cut' });
    if (p.editFlags.canCopy) items.push({ role: 'copy' });
    if (p.editFlags.canPaste) items.push({ role: 'paste' });
    if (p.editFlags.canSelectAll) items.push({ role: 'selectAll' });
  } else if (p.selectionText.trim() !== '' && p.editFlags.canCopy) {
    items.push({ role: 'copy' });
  }
  return items;
}
