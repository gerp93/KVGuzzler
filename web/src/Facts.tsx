// A short row of labelled figures under a page's tiles or inside a card: the quiet label above, the value below. In place of a sentence that
// says the same thing.
export interface Fact {
  label: string;
  value: string;
}

export function Facts({ items }: { items: Fact[] }) {
  if (items.length === 0) return null;
  return (
    <dl class="facts">
      {items.map((f) => (
        <div key={f.label}>
          <dt>{f.label}</dt>
          <dd>{f.value}</dd>
        </div>
      ))}
    </dl>
  );
}
