// What shows while the first answer from the server is on its way: the shape of the page, not a lone "Loading…".
export function Skeleton() {
  return (
    <div class="skel" aria-busy="true" aria-label="Loading">
      <div />
      <div />
      <div />
    </div>
  );
}
