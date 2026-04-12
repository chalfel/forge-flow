'use client'

import { useState } from 'react'
import { createClient } from '@/lib/supabase/client'

export default function DevicePage() {
  const [userCode, setUserCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const supabase = createClient()

  async function handleAuthorize(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError(null)

    // Check if user is logged in
    const { data: { user } } = await supabase.auth.getUser()
    if (!user) {
      setError('Please log in first')
      setLoading(false)
      return
    }

    // Authorize the device code
    const response = await fetch('/api/auth/device/authorize', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_code: userCode.toUpperCase() }),
    })

    if (!response.ok) {
      const data = await response.json()
      setError(data.error || 'Failed to authorize')
      setLoading(false)
      return
    }

    setSuccess(true)
    setLoading(false)
  }

  if (success) {
    return (
      <main className="min-h-screen flex items-center justify-center p-8">
        <div className="text-center space-y-4">
          <div className="w-16 h-16 bg-green-500/20 rounded-full flex items-center justify-center mx-auto">
            <svg className="w-8 h-8 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <h1 className="text-2xl font-bold">Device Authorized</h1>
          <p className="text-muted-foreground">
            You can close this window and return to your terminal.
          </p>
        </div>
      </main>
    )
  }

  return (
    <main className="min-h-screen flex items-center justify-center p-8">
      <div className="w-full max-w-sm space-y-6">
        <div className="text-center">
          <h1 className="text-2xl font-bold">Authorize Device</h1>
          <p className="text-muted-foreground mt-2">
            Enter the code shown in your terminal
          </p>
        </div>

        <form onSubmit={handleAuthorize} className="space-y-4">
          <div>
            <input
              type="text"
              value={userCode}
              onChange={(e) => setUserCode(e.target.value.toUpperCase())}
              className="w-full px-4 py-3 bg-secondary border border-border rounded-lg text-center text-2xl font-mono tracking-widest focus:outline-none focus:ring-2 focus:ring-primary"
              placeholder="XXXXXXXX"
              maxLength={8}
              required
            />
          </div>

          {error && (
            <p className="text-sm text-red-500 text-center">{error}</p>
          )}

          <button
            type="submit"
            disabled={loading || userCode.length !== 8}
            className="w-full py-3 bg-primary text-primary-foreground rounded-lg font-medium hover:opacity-90 transition disabled:opacity-50"
          >
            {loading ? 'Authorizing...' : 'Authorize'}
          </button>
        </form>
      </div>
    </main>
  )
}
