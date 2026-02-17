export function Spinner({ className = '' }: { className?: string }) {
  const value = className.trim() ? `spinner ${className.trim()}` : 'spinner';
  return <span className={value} aria-hidden="true" />;
}
