import type { User } from "@/api/types";

// getInitials returns the 1-2 letter monogram used for the avatar
// circle in Sidebar and TopBar when no avatar image is configured.
//
// Rules:
//   - Prefer display_name: first letter of each whitespace-separated
//     token, joined and uppercased, capped at 2 chars. "Alex Zafra"
//     → "AZ", "Pedro" → "P".
//   - Fall back to the first 2 chars of username, uppercased.
//   - "?" if neither is available (a user object missing both is a
//     defensive fallback — shouldn't happen with a real session).
//
// This was duplicated verbatim across Sidebar.tsx and TopBar.tsx for
// months. Extracted on 2026-04-30 so when the avatar surface grows
// (e.g. rendering avatar_path when the backend has it), one helper
// updates both call sites.
export function getInitials(user: Pick<User, "display_name" | "username"> | null | undefined): string {
  if (!user) return "?";
  if (user.display_name) {
    const fromName = user.display_name
      .split(/\s+/)
      .filter(Boolean)
      .map((token) => token[0])
      .join("")
      .toUpperCase()
      .slice(0, 2);
    if (fromName) return fromName;
  }
  if (user.username) {
    return user.username.slice(0, 2).toUpperCase();
  }
  return "?";
}
