import type { ReactNode } from "react";
import { LogoMark, Wordmark } from "./logo";

/** Centered layout for setup, sign-in, invite, and forced-change screens. */
export function AuthLayout({ children, footer }: { children: ReactNode; footer?: ReactNode }) {
  return (
    <div className="bg-bg flex min-h-full flex-col items-center justify-center px-4 py-10">
      <div className="mb-6 flex items-center gap-2.5">
        <LogoMark />
        <Wordmark className="text-lg" />
      </div>
      <div className="card w-full max-w-sm p-6 shadow-2 animate-[hh-slide-up_240ms_var(--ease-out-quick)]">
        {children}
      </div>
      {footer && <div className="text-fg-subtle mt-5 text-[13px]">{footer}</div>}
    </div>
  );
}
