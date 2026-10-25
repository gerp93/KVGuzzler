// Small line icons for the tab bar, drawn in the text colour so every theme can use them.
import type { ComponentChildren } from 'preact';

function Svg({ children }: { children: ComponentChildren }) {
  return (
    <svg class="ico" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      {children}
    </svg>
  );
}

const ICONS = {
  live: (
    <Svg>
      <path d="M13 2 4 14h7l-1 8 9-12h-7z" />
    </Svg>
  ),
  history: (
    <Svg>
      <path d="M3 3v18h18" />
      <path d="m7 15 4-5 3 3 5-7" />
    </Svg>
  ),
  hierarchy: (
    <Svg>
      <rect x="9" y="3" width="6" height="5" rx="1" />
      <rect x="3" y="16" width="6" height="5" rx="1" />
      <rect x="15" y="16" width="6" height="5" rx="1" />
      <path d="M12 8v4M6 16v-4h12v4" />
    </Svg>
  ),
  settings: (
    <Svg>
      <path d="M4 7h10M18 7h2M4 17h2M10 17h10" />
      <circle cx="16" cy="7" r="2" />
      <circle cx="8" cy="17" r="2" />
    </Svg>
  ),
  help: (
    <Svg>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.7.4-1 .9-1 1.7M12 17h.01" />
    </Svg>
  ),
};

export function TabIcon({ name }: { name: keyof typeof ICONS }) {
  return ICONS[name];
}

// Small icons for the figure tiles: smaller than the tab bar's, in the text colour.
const FIGURES = {
  now: <path d="M13 2 4 14h7l-1 8 9-12h-7z" />,
  energy: (
    <>
      <rect x="3" y="8" width="16" height="9" rx="2" />
      <path d="M21 11v3M7 11v3M11 11v3" />
    </>
  ),
  cost: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M14.5 9.2c-.4-.8-1.4-1.2-2.5-1.2-1.4 0-2.5.7-2.5 1.8s1 1.6 2.5 2 2.5.9 2.5 2-1.1 1.9-2.5 1.9c-1.2 0-2.2-.5-2.6-1.4M12 6.5V8m0 8v1.5" />
    </>
  ),
  peak: <path d="M3 17 9 11l4 4 8-9M15 6h6v6" />,
};

export function FigureIcon({ name }: { name: keyof typeof FIGURES }) {
  return (
    <svg class="kpi-ico" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      {FIGURES[name]}
    </svg>
  );
}
