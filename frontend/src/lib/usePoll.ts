"use client";

import { useCallback, useEffect, useRef, useState } from "react";

export interface Poll<T> {
  data: T | undefined;
  error: Error | undefined;
  loading: boolean;
  updatedAt: number | undefined;
  refresh: () => void;
}

/**
 * Fetches `fn` immediately and then every `intervalMs` while the tab is visible.
 * Keeps the last good data on error so a blip doesn't blank the screen.
 */
export function usePoll<T>(fn: () => Promise<T>, intervalMs = 5000, deps: unknown[] = []): Poll<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<Error>();
  const [loading, setLoading] = useState(true);
  const [updatedAt, setUpdatedAt] = useState<number>();
  const fnRef = useRef(fn);
  useEffect(() => {
    fnRef.current = fn;
  });

  const run = useCallback(async () => {
    try {
      const result = await fnRef.current();
      setData(result);
      setError(undefined);
      setUpdatedAt(Date.now());
    } catch (e) {
      setError(e instanceof Error ? e : new Error(String(e)));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    run();
    if (intervalMs <= 0) return;
    const id = setInterval(() => {
      if (document.visibilityState === "visible") run();
    }, intervalMs);
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run, intervalMs, ...deps]);

  return { data, error, loading, updatedAt, refresh: run };
}

/**
 * Current time, re-rendered every `ms` so "updated 3s ago" labels stay fresh.
 * Returns 0 until mounted: reading the clock during render would bake the
 * prerender time into the page.
 */
export function useNow(ms = 1000) {
  const [now, setNow] = useState(0);
  useEffect(() => {
    const tick = () => setNow(Date.now());
    const first = setTimeout(tick, 0);
    const id = setInterval(tick, ms);
    return () => {
      clearTimeout(first);
      clearInterval(id);
    };
  }, [ms]);
  return now;
}
