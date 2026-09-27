import { useEffect, useState } from "react";

/** Phones and narrow windows get the reader, sheets and the overflow menu. */
export const NARROW = "(max-width: 760px)";
/** Below this the topbar's secondary controls fold into an overflow menu. */
export const COMPACT = "(max-width: 1320px)";

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== "undefined" && typeof window.matchMedia === "function"
      ? window.matchMedia(query).matches
      : false);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const list = window.matchMedia(query);
    const update = () => setMatches(list.matches);
    update();
    list.addEventListener("change", update);
    return () => list.removeEventListener("change", update);
  }, [query]);
  return matches;
}
