import './globals.css';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'PersonaWorlds',
  description: 'Human-in-the-loop AI personas in interest rooms'
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
