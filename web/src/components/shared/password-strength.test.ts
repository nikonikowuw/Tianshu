import { describe, expect, it } from 'vitest'
import { passwordLevel, scorePassword } from './password-strength'

describe('scorePassword', () => {
  it('scores empty input as 0', () => {
    expect(scorePassword('')).toBe(0)
  })

  it('rewards length at the 8/12/16 thresholds', () => {
    expect(scorePassword('abcdefg')).toBe(0)
    expect(scorePassword('abcdefgh')).toBe(1)
    expect(scorePassword('abcdefghijkl')).toBe(2)
    expect(scorePassword('abcdefghijklmnop')).toBe(3)
  })

  it('adds one point for three or more character classes', () => {
    expect(scorePassword('abcdefgh')).toBe(1)
    expect(scorePassword('Abcdefgh')).toBe(1)
    expect(scorePassword('Abcdefg1')).toBe(2)
    expect(scorePassword('Abcdefg1!')).toBe(2)
  })
})

describe('passwordLevel', () => {
  it('returns null while the score is still 0', () => {
    expect(passwordLevel('')).toBeNull()
    expect(passwordLevel('a')).toBeNull()
    expect(passwordLevel('abcdefgh')).not.toBeNull()
  })

  it('maps scores to display levels', () => {
    expect(passwordLevel('abcdefgh')).toBe('weak')
    expect(passwordLevel('abcdefghijkl')).toBe('fair')
    expect(passwordLevel('abcdefghijklmnop')).toBe('good')
    expect(passwordLevel('Abcdefghijklmnop!')).toBe('strong')
  })
})
