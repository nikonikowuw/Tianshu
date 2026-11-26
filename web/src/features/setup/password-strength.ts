export type PasswordStrengthLevel = 'weak' | 'fair' | 'good' | 'strong'

/**
 * Heuristic password score (0–4) mirroring common policy advice.
 *
 * This is a UX hint only: the server enforces the hard 8–128 rule. Scoring
 * rewards length first, then character-class variety.
 */
export function scorePassword(value: string): number {
  if (!value) return 0
  let score = 0
  if (value.length >= 8) score += 1
  if (value.length >= 12) score += 1
  if (value.length >= 16) score += 1
  const classes = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter((re) =>
    re.test(value)
  ).length
  if (classes >= 3) score += 1
  // Length contributes at most 3 points, variety 1: the raw score is already 0–4.
  return score
}

/** Map a raw password to a display level, or `null` when empty. */
export function passwordLevel(value: string): PasswordStrengthLevel | null {
  const score = scorePassword(value)
  if (score <= 0) return null
  if (score === 1) return 'weak'
  if (score === 2) return 'fair'
  if (score === 3) return 'good'
  return 'strong'
}
