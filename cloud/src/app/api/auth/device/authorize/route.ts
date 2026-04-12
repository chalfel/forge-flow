import { NextRequest, NextResponse } from 'next/server'
import { createClient } from '@/lib/supabase/server'
import { createServiceClient } from '@/lib/supabase/server'

// POST /api/auth/device/authorize
// Authorizes a device code (called from the web UI)
export async function POST(req: NextRequest) {
  const supabase = createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) {
    return NextResponse.json({ error: 'not authenticated' }, { status: 401 })
  }

  const body = await req.json()
  const { user_code } = body

  if (!user_code) {
    return NextResponse.json({ error: 'missing user_code' }, { status: 400 })
  }

  const serviceClient = createServiceClient()

  // Find the device code
  const { data: deviceCode, error } = await serviceClient
    .from('device_codes')
    .select('*')
    .eq('user_code', user_code.toUpperCase())
    .eq('status', 'pending')
    .single()

  if (error || !deviceCode) {
    return NextResponse.json({ error: 'invalid or expired code' }, { status: 400 })
  }

  // Check expiration
  if (new Date(deviceCode.expires_at) < new Date()) {
    return NextResponse.json({ error: 'code expired' }, { status: 400 })
  }

  // Authorize it
  const { error: updateError } = await serviceClient
    .from('device_codes')
    .update({
      user_id: user.id,
      status: 'authorized',
    })
    .eq('id', deviceCode.id)

  if (updateError) {
    return NextResponse.json({ error: 'failed to authorize' }, { status: 500 })
  }

  return NextResponse.json({ success: true })
}
