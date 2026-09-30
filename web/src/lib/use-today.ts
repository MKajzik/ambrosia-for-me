import { useEffect, useState } from "react";
import { today } from "@/lib/dates";

/** Today's local calendar date, re-read every `pollMs`, so a page left open past midnight moves on to the new day. */
export function useToday(pollMs = 60_000): string {
  const [date, setDate] = useState(() => today());
  useEffect(() => {
    const timer = setInterval(() => setDate(today()), pollMs);
    return () => clearInterval(timer);
  }, [pollMs]);
  return date;
}
