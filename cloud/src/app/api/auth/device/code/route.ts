import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'
import { randomBytes } from 'crypto'

// POST /api/auth/device/code
// Initiates device code flow for CLI login
export async function POST(req: NextRequest) {
  const supabase = createServiceClient()

  // Generate codes
  const deviceCode = randomBytes(32).toString('hex')
  const userCode = randomBytes(4).toString('hex').toUpperCase().slice(0, 8)

  // Insert into database
  const expiresAt = new Date(Date.now() + 15 * 60 * 1000) // 15 minutes

  const { error } = await supabase.from('device_codes').insert({
    device_code: deviceCode,
    user_code: userCode,
    expires_at: expiresAt.toISOString(),
    status: 'pending',
  })

  if (error) {
    return NextResponse.json({ error: 'Failed to create device code' }, { status: 500 })
  }

  const appUrl = process.env.NEXT_PUBLIC_APP_URL || 'http://localhost:3000'

  return NextResponse.json({
    device_code: deviceCode,
    user_code: userCode,
    verification_url: `${appUrl}/device`,
    expires_in: 900, // 15 minutes
    interval: 5, // poll every 5 seconds
  })
}
