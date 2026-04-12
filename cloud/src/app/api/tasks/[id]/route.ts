import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'

// GET /api/tasks/[id]
export async function GET(
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

  // Get task
  const { data: task, error } = await supabase
    .from('tasks')
    .select(`
      *,
      specs(*),
      repos(repo_id, name, git_url)
    `)
    .eq('id', params.id)
    .single()

  if (error || !task) {
    return NextResponse.json({ error: 'not found' }, { status: 404 })
  }

  return NextResponse.json({
    id: task.id,
    specId: task.spec_id,
    name: task.name,
    status: task.status,
    priority: task.priority,
    repo: (task.repos as any)?.repo_id || null,
    assignedTo: task.assigned_to,
    branch: task.branch,
    pr: task.pr_url,
    deps: task.deps,
    touches: task.touches,
    doneWhen: task.done_when,
    createdAt: task.created_at,
    updatedAt: task.updated_at,
    startedAt: task.started_at,
    completedAt: task.completed_at,
  })
}
