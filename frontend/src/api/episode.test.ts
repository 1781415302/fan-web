import { beforeEach, describe, expect, it } from 'vitest'

import { getSubtitleUrl, getStreamUrl } from './episode'

const tokenKey = 'fan_web_token'

describe('episode media URLs', () => {
  beforeEach(() => {
    window.localStorage.clear()
    window.localStorage.setItem(tokenKey, 'SHOULD-NOT-APPEAR')
  })

  it('stream URL uses media_token and never embeds login JWT', () => {
    const url = getStreamUrl(7, 'media-token-example')
    expect(url).toContain('media_token=media-token-example')
    expect(url).toContain('/episodes/7/stream')
    expect(url).not.toContain('SHOULD-NOT-APPEAR')
    expect(url).not.toMatch(/[?expect(url).not.toContain('token=')]token=/)
  })

  it('subtitle URL uses media_token with track', () => {
    const url = getSubtitleUrl(7, 3, 'media-token-example')
    expect(url).toContain('media_token=media-token-example')
    expect(url).toContain('track=3')
    expect(url).not.toContain('SHOULD-NOT-APPEAR')
    expect(url).not.toMatch(/[?expect(url).not.toContain('token=')]token=/)
  })
})