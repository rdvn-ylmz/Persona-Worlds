import Link from 'next/link';

export function TopNav() {
  return (
    <nav className="top-nav" aria-label="Main navigation">
      <div className="top-nav-inner">
        <Link className="top-nav-brand" href="/">
          PersonaWorlds
        </Link>
        <div className="top-nav-links">
          <Link href="/#feed">Feed</Link>
          <Link href="/#rooms">Rooms</Link>
          <Link href="/templates">Templates</Link>
          <Link href="/#profile">Profile</Link>
        </div>
      </div>
    </nav>
  );
}
