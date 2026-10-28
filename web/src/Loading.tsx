// The page's figures being read for a newly picked period: the last period's figures stay under it, dimmed, so they do not pass for the new ones.
export function PeriodLoading({ span }: { span: string }) {
  return (
    <div class="period-loading" role="status">
      <span class="spinner" aria-hidden="true" />
      <span>Loading {span}…</span>
    </div>
  );
}
