import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";
import type { NodeState } from "../api/types";
import { STATE_WAVELENGTH, wl } from "../lib/format";

const cx = (...c: Array<string | false | null | undefined>) => c.filter(Boolean).join(" ");

/* ------------------------------------------------------------------ Button */

type ButtonVariant = "primary" | "secondary" | "destructive";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: "sm" | "md";
  /** Shows the busy label and sets aria-busy; the button stays focusable. */
  busy?: boolean;
  busyLabel?: string;
}

const VARIANT: Record<ButtonVariant, string> = {
  // Bone on charcoal: the one filled control on any panel.
  primary: "bg-bone text-ground border-bone hover:bg-bone-2 hover:border-bone-2",
  secondary: "bg-transparent text-bone border-seam-strong hover:border-bone-3 hover:bg-plate",
  // Destructive stays an outline; its wavelength only appears as ink and on hover/focus.
  destructive:
    "bg-transparent text-nm656 border-seam-strong hover:border-nm656 focus-visible:border-nm656 focus-visible:outline-nm656",
};

export function Button({ variant = "secondary", size = "md", busy, busyLabel, className, children, disabled, ...rest }: ButtonProps) {
  return (
    <button
      type="button"
      {...rest}
      disabled={disabled}
      aria-busy={busy || undefined}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-[3px] border font-medium tracking-[0.02em]",
        "transition-[background-color,border-color,color] duration-150 active:translate-y-px",
        "disabled:cursor-not-allowed disabled:opacity-45 disabled:active:translate-y-0",
        size === "sm" ? "h-8 px-3 text-[13px]" : "h-9 px-3.5 text-sm",
        VARIANT[variant],
        className,
      )}
    >
      {busy && busyLabel ? busyLabel : children}
    </button>
  );
}

/* ------------------------------------------------------------------- Panel */

interface PanelProps {
  title: string;
  /** Right-aligned header content: filters, hints. */
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
  testId?: string;
}

/** An ash plate: the only container in the world. Titled by a legend, divided by seams. */
export function Panel({ title, aside, children, className, testId }: PanelProps) {
  const id = `panel-${title.toLowerCase().replace(/\W+/g, "-")}`;
  return (
    <section aria-labelledby={id} data-testid={testId} className={cx("flex min-w-0 flex-col border-t border-seam bg-plate", className)}>
      <header className="flex min-h-11 items-center justify-between gap-3 px-4 pt-3 pb-2">
        <h2 id={id} className="legend !text-bone-2">{title}</h2>
        {aside}
      </header>
      <div className="flex min-h-0 flex-1 flex-col px-4 pb-4">{children}</div>
    </section>
  );
}

/* ------------------------------------------------------------------- Field */

interface FieldProps {
  label: string;
  children: ReactNode;
  className?: string;
}

export function Field({ label, children, className }: FieldProps) {
  return (
    <label className={cx("grid min-w-0 gap-1", className)}>
      <span className="legend">{label}</span>
      {children}
    </label>
  );
}

const control =
  "h-9 w-full min-w-0 rounded-[3px] border border-seam-strong bg-ground px-2.5 text-sm text-bone placeholder:text-bone-3 " +
  "hover:border-bone-3 focus-visible:border-nm589 focus-visible:outline-none";

export function TextInput({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input autoComplete="off" spellCheck={false} {...rest} className={cx(control, className)} />;
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select {...rest} className={cx(control, "pr-7", className)}>
      {children}
    </select>
  );
}

/* --------------------------------------------------------------- LineGlyph */

/**
 * A node state drawn as line form, the world's state vocabulary:
 * leader doubled, follower solid, candidate dashed, partitioned half-height
 * dotted, down struck. Decorative: callers always print the state as text too.
 */
export function LineGlyph({ state, height = 20 }: { state: NodeState; height?: number }) {
  const c = wl(STATE_WAVELENGTH[state]);
  const h = height;
  return (
    <svg width="14" height={h} viewBox={`0 0 14 ${h}`} aria-hidden="true" className="shrink-0">
      {state === "leader" && (
        <>
          <line x1="5" x2="5" y1="1" y2={h - 1} stroke={c} strokeWidth="1.5" />
          <line x1="9" x2="9" y1="1" y2={h - 1} stroke={c} strokeWidth="1.5" />
        </>
      )}
      {state === "follower" && <line x1="7" x2="7" y1="1" y2={h - 1} stroke={c} strokeWidth="1.5" />}
      {state === "candidate" && <line x1="7" x2="7" y1="1" y2={h - 1} stroke={c} strokeWidth="1.5" strokeDasharray="3 2" />}
      {state === "partition" && <line x1="7" x2="7" y1={h / 2} y2={h - 1} stroke={c} strokeWidth="1.5" strokeDasharray="1 2" />}
      {state === "down" && (
        <>
          <line x1="7" x2="7" y1="1" y2={h - 1} stroke={c} strokeWidth="1" opacity="0.45" />
          <line x1="2" x2="12" y1={h / 2 + 4} y2={h / 2 - 4} stroke={c} strokeWidth="1.5" />
        </>
      )}
    </svg>
  );
}

export { cx };
