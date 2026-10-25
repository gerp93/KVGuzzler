// Pure helper for the big figures (stat tiles): a value like "661.4 kWh" is shown as a large number with its unit small and quiet beside it.

/** Splits a formatted figure into the number (with whatever sign or currency comes before it) and its unit: "661.4 kWh" gives ["661.4", "kWh"],
 * "$104.51" gives ["$104.51", ""], "≤ 14.93 kW" gives ["≤ 14.93", "kW"], and a dash stays a dash. */
export function splitFigure(text: string): [string, string] {
  const m = /^(.*?\d[\d.,]*)\s*([^\d\s].*)?$/.exec(text.trim());
  if (!m) return [text, ''];
  return [m[1]!, m[2] ?? ''];
}
