// A short row of labelled figures under a page's tiles or inside a card: the quiet label above, the value below. In place of a sentence that
// says the same thing.
import { useState } from 'preact/hooks';

export interface Fact {
  label: string;
  value: string;
  /** The value in full, when what is shown is an abbreviation of it: shown on hover (the tooltip) and, by tapping the value, under it. */
  full?: string;
}

function FactValue({ fact }: { fact: Fact }) {
  const [open, setOpen] = useState(false);
  if (!fact.full) return <>{fact.value}</>;
  return (
    <>
      <button type="button" class="fact-short" title={fact.full} aria-expanded={open} onClick={() => setOpen(!open)}>
        {fact.value}
      </button>
      {open && <span class="fact-full">{fact.full}</span>}
    </>
  );
}

/** `table`: a two-column table, the label on the left and its value on the right, for the details of a page. With a `title` it is headed.
 *  `footer`: the foot of a figure tile, under a line, each label above its value. */
export function Facts({ items, table, title, footer }: { items: Fact[]; table?: boolean; title?: string; footer?: boolean }) {
  if (items.length === 0) return null;
  const list = (
    <dl class={footer ? 'facts foot' : table ? 'facts table' : 'facts'}>
      {items.map((f) => (
        <div key={f.label}>
          <dt>{f.label}</dt>
          <dd>
            <FactValue fact={f} />
          </dd>
        </div>
      ))}
    </dl>
  );
  return table && title ? (
    <section class="facts-card">
      <h3>{title}</h3>
      {list}
    </section>
  ) : (
    list
  );
}
