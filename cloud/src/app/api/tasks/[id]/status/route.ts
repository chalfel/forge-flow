import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'

// PATCH /api/tasks/[id]/status
export async function PATCH(
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
  const { status, branch, pr } = body

  // Build update object
  const update: any = {
    status,
    updated_at: new Date().toISOString(),
  }

  if (branch) update.branch = branch
  if (pr) update.pr_url = pr

  if (status === 'in_progress' && !update.started_at) {
    update.started_at = new Date().toISOString()
  }
  if (status === 'done') {
    update.completed_at = new Date().toISOString()
  }

  // Update task
  const { error } = await supabase
    .from('tasks')
    .update(update)
    .eq('id', params.id)

  if (error) {
    return NextResponse.json({ error: error.message }, { status: 500 })
  }

  return NextResponse.json({ success: true })
}
