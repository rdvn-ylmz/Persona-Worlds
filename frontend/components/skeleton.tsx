type SkeletonListProps = {
  rows?: number;
  className?: string;
};

export function SkeletonList({ rows = 3, className = '' }: SkeletonListProps) {
  const count = Math.max(1, rows);
  const wrapperClassName = className.trim() ? `skeleton-list ${className.trim()}` : 'skeleton-list';

  return (
    <div className={wrapperClassName}>
      {Array.from({ length: count }).map((_, idx) => (
        <article key={`skeleton-${idx}`} className="skeleton-card" aria-hidden="true">
          <div className="skeleton skeleton-line-lg" />
          <div className="skeleton skeleton-line" />
          <div className="skeleton skeleton-line" />
          <div className="skeleton skeleton-line-sm" />
        </article>
      ))}
    </div>
  );
}
