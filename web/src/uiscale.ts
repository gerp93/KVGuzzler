/** How much bigger than 16 px the page's text is: the page grows its text on a very large monitor, and what is drawn in pixels
 * (the chart's axes, the flow chart's labels) has to grow with it. */
export function uiScale(): number {
  if (typeof document === 'undefined') return 1;
  const px = parseFloat(getComputedStyle(document.documentElement).fontSize);
  return Number.isFinite(px) && px > 0 ? px / 16 : 1;
}
