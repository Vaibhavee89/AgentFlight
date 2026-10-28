/** Pretty-prints a decoded JSON value with two-space indentation. */
export function prettyJSON(value: unknown): string {
  // JSON.stringify returns undefined for undefined input, which the typings omit.
  const text = JSON.stringify(value, null, 2) as string | undefined;
  return text ?? "null";
}

export interface Truncated {
  text: string;
  truncated: boolean;
  totalChars: number;
}

/** Cuts text to limit characters and reports whether anything was removed. */
export function truncate(text: string, limit: number): Truncated {
  if (text.length <= limit) return { text, truncated: false, totalChars: text.length };
  return { text: text.slice(0, limit), truncated: true, totalChars: text.length };
}
