'use client';

import { createContext, ReactNode, useCallback, useContext, useMemo, useState } from 'react';

type ToastKind = 'success' | 'error' | 'info';

type ToastItem = {
  id: number;
  kind: ToastKind;
  text: string;
};

type ToastContextValue = {
  success: (text: string) => void;
  error: (text: string) => void;
  info: (text: string) => void;
  dismiss: (id: number) => void;
};

const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);

  const dismiss = useCallback((id: number) => {
    setItems((current) => current.filter((item) => item.id !== id));
  }, []);

  const push = useCallback(
    (kind: ToastKind, text: string) => {
      const message = (text || '').trim();
      if (!message) {
        return;
      }

      const id = Date.now() + Math.floor(Math.random() * 1000);
      setItems((current) => [...current, { id, kind, text: message }]);

      window.setTimeout(() => {
        dismiss(id);
      }, 4200);
    },
    [dismiss]
  );

  const value = useMemo<ToastContextValue>(
    () => ({
      success: (text) => push('success', text),
      error: (text) => push('error', text),
      info: (text) => push('info', text),
      dismiss
    }),
    [dismiss, push]
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-container" role="status" aria-live="polite">
        {items.map((item) => (
          <article key={item.id} className={`toast toast-${item.kind}`}>
            <span>{item.text}</span>
            <button type="button" className="toast-close" onClick={() => dismiss(item.id)} aria-label="Dismiss toast">
              ×
            </button>
          </article>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    return {
      success: (_text: string) => undefined,
      error: (_text: string) => undefined,
      info: (_text: string) => undefined,
      dismiss: (_id: number) => undefined
    } satisfies ToastContextValue;
  }
  return context;
}
