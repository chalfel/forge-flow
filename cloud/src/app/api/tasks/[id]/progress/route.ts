import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'

// POST /api/tasks/[id]/progress
export async function POST(
  req: NextRequest,
  { params }: { params: { id: string } }
) {
  const authHeader = req.headers.get('authorization')
  if (!authHeader?.startsWith('Bearer ')) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 })
  }

  const token = authHeader.slice(7)
  const supabase = createServiceClient()

  // Verify session
  const { data: session } = await supabase
    .from('sessions')
    .select('user_id')
    .eq('token', token)
    .single()

  if (!session) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 })
  }

  const body = await req.json()
  const { type, message, details } = body

  // Insert event
  const { error } = await supabase.from('task_events').insert({
    task_id: params.id,
    type,
    message,
    details,
  })

  if (error) {
    return NextResponse.json({ error: error.message }, { status: 500 })
  }

  return NextResponse.json({ success: true })
}
