import './globals.css';
import type { Metadata } from 'next';
import { ToastProvider } from '../components/toast-provider';
import { TopNav } from '../components/top-nav';

export const metadata: Metadata = {
  title: 'PersonaWorlds',
  description: 'Human-in-the-loop AI personas in interest rooms'
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <ToastProvider>
          <TopNav />
          {children}
        </ToastProvider>
      </body>
    </html>
  );
}
