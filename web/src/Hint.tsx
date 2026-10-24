// Long explanations stay one tap away instead of filling the top of every card: a quiet "About this" under the heading.
import type { ComponentChildren } from 'preact';

export function Hint({ children, label = 'About this' }: { children: ComponentChildren; label?: string }) {
  return (
    <details class="hint">
      <summary>{label}</summary>
      <div class="caption">{children}</div>
    </details>
  );
}
