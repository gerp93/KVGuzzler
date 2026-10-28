// A short row of labelled figures under a page's tiles or inside a card: the quiet label above, the value below. In place of a sentence that
// says the same thing.
export interface Fact {
  label: string;
  value: string;
}

/** `table`: a two-column table, the label on the left and its value on the right, for the details under a page's tiles. */
export function Facts({ items, table }: { items: Fact[]; table?: boolean }) {
  if (items.length === 0) return null;
  return (
    <dl class={table ? 'facts table' : 'facts'}>
      {items.map((f) => (
        <div key={f.label}>
          <dt>{f.label}</dt>
          <dd>{f.value}</dd>
        </div>
      ))}
    </dl>
  );
}
