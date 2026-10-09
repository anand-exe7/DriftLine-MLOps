"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, useSyncExternalStore } from "react";
import { Activity, Bell, BookOpen, Boxes, FlaskConical, LayoutDashboard, Menu, Moon, Sun, Waves, X } from "lucide-react";
import { cx } from "./ui";

const NAV = [
  { href: "/", label: "Overview", Icon: LayoutDashboard },
  { href: "/models", label: "Model Registry", Icon: Boxes },
  { href: "/predict", label: "Prediction Lab", Icon: FlaskConical },
  { href: "/monitoring", label: "Live Monitoring", Icon: Activity },
  { href: "/alerts", label: "Alerts", Icon: Bell },
  { href: "/guide", label: "How it works", Icon: BookOpen },
];

const subscribeTheme = (cb: () => void) => {
  const obs = new MutationObserver(cb);
  obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  return () => obs.disconnect();
};

function ThemeToggle() {
  // The inline script in layout.tsx sets data-theme before first paint; this just reads it.
  const theme = useSyncExternalStore(
    subscribeTheme,
    () => document.documentElement.dataset.theme ?? "light",
    () => null,
  );

  const flip = () => {
    const next = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    try {
      localStorage.setItem("driftline-theme", next);
    } catch {}
  };

  return (
    <button
      onClick={flip}
      className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-ink-2 hover:bg-surface-2 hover:text-ink"
      aria-label="Toggle color theme"
    >
      {theme === "dark" ? <Sun size={16} aria-hidden /> : <Moon size={16} aria-hidden />}
      {theme === "dark" ? "Light mode" : "Dark mode"}
    </button>
  );
}

export function Sidebar() {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);

  const isActive = (href: string) => (href === "/" ? pathname === "/" : pathname.startsWith(href));

  const nav = (
    <nav className="flex h-full flex-col gap-1 p-3">
      <Link href="/" className="mb-4 flex items-center gap-2.5 px-3 pt-2">
        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent text-white">
          <Waves size={18} aria-hidden />
        </span>
        <span>
          <span className="block text-[15px] font-semibold leading-tight text-ink">Driftline</span>
          <span className="block text-[11px] text-ink-3">MLOps control plane</span>
        </span>
      </Link>
      {NAV.map(({ href, label, Icon }) => (
        <Link
          key={href}
          href={href}
          onClick={() => setOpen(false)}
          className={cx(
            "flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition",
            isActive(href) ? "bg-accent-soft font-medium text-accent" : "text-ink-2 hover:bg-surface-2 hover:text-ink",
          )}
          aria-current={isActive(href) ? "page" : undefined}
        >
          <Icon size={16} aria-hidden />
          {label}
        </Link>
      ))}
      <div className="mt-auto border-t border-line pt-3">
        <ThemeToggle />
      </div>
    </nav>
  );

  return (
    <>
      {/* mobile top bar */}
      <div className="sticky top-0 z-30 flex items-center justify-between border-b border-line bg-surface px-4 py-3 lg:hidden">
        <span className="flex items-center gap-2 font-semibold text-ink">
          <Waves size={18} className="text-accent" aria-hidden /> Driftline
        </span>
        <button onClick={() => setOpen((o) => !o)} aria-label="Toggle navigation" className="text-ink-2">
          {open ? <X size={20} /> : <Menu size={20} />}
        </button>
      </div>
      {open && <div className="fixed inset-0 z-30 bg-black/40 lg:hidden" onClick={() => setOpen(false)} />}
      <aside
        className={cx(
          "fixed inset-y-0 left-0 z-40 w-60 border-r border-line bg-surface transition-transform lg:sticky lg:top-0 lg:h-screen lg:translate-x-0",
          open ? "translate-x-0" : "-translate-x-full",
        )}
      >
        {nav}
      </aside>
    </>
  );
}
