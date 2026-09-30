/** An 8-character code as people read it aloud: capitals, in groups of four. */
export function formatInviteCode(code: string): string {
  return (code.toUpperCase().match(/.{1,4}/g) ?? []).join("-");
}

const DATE = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });
const WHEN = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short" });

export const formatDate = (iso: string): string => DATE.format(new Date(iso));
export const formatWhen = (iso: string): string => WHEN.format(new Date(iso));
