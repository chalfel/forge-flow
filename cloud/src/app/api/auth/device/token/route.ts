import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'
import { randomBytes } from 'crypto'

// POST /api/auth/device/token
// Polls for token after user authorizes device code
export async function POST(req: NextRequest) {
  const supabase = createServiceClient()
  const body = await req.json()
  const { device_code } = body

  if (!device_code) {
    return NextResponse.json({ error: 'missing device_code' }, { status: 400 })
  }

  // Look up device code
  const { data: deviceCodeRecord, error } = await supabase
    .from('device_codes')
    .select('*')
    .eq('device_code', device_code)
    .single()

  if (error || !deviceCodeRecord) {
    return NextResponse.json({ error: 'invalid_grant' }, { status: 400 })
  }

  // Check if expired
  if (new Date(deviceCodeRecord.expires_at) < new Date()) {
    return NextResponse.json({ error: 'expired_token' }, { status: 400 })
  }

  // Update last polled
  await supabase
    .from('device_codes')
    .update({ last_polled_at: new Date().toISOString() })
    .eq('id', deviceCodeRecord.id)

  // Check status
  if (deviceCodeRecord.status === 'pending') {
    return NextResponse.json({ error: 'authorization_pending' }, { status: 400 })
  }

  if (deviceCodeRecord.status === 'expired') {
    return NextResponse.json({ error: 'expired_token' }, { status: 400 })
  }

  if (deviceCodeRecord.status !== 'authorized' || !deviceCodeRecord.user_id) {
    return NextResponse.json({ error: 'access_denied' }, { status: 400 })
  }

  // Get user info
  const { data: { user } } = await supabase.auth.admin.getUserById(deviceCodeRecord.user_id)

  if (!user) {
    return NextResponse.json({ error: 'user_not_found' }, { status: 400 })
  }

  // Generate access token (in production, use proper JWT)
  const accessToken = randomBytes(32).toString('hex')
  const refreshToken = randomBytes(32).toString('hex')

  // Store session
  await supabase.from('sessions').insert({
    user_id: user.id,
    token: accessToken,
    expires_at: new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString(), // 7 days
  })

  // Get user's team
  const { data: membership } = await supabase
    .from('team_members')
    .select('team_id')
    .eq('user_id', user.id)
    .single()

  // Delete the device code (one-time use)
  await supabase.from('device_codes').delete().eq('id', deviceCodeRecord.id)

  return NextResponse.json({
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_in: 7 * 24 * 60 * 60, // 7 days
    user_id: user.id,
    email: user.email,
    team_id: membership?.team_id || null,
  })
}
