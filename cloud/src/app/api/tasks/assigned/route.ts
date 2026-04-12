import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'

// GET /api/tasks/assigned
// Returns tasks assigned to the authenticated user
export async function GET(req: NextRequest) {
  const authHeader = req.headers.get('authorization')
  if (!authHeader?.startsWith('Bearer ')) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 })
  }

  const token = authHeader.slice(7)
  const supabase = createServiceClient()

  // Look up session
  const { data: session } = await supabase
    .from('sessions')
    .select('user_id, expires_at')
    .eq('token', token)
    .single()

  if (!session || new Date(session.expires_at) < new Date()) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 })
  }

  // Get assigned tasks
  const { data: tasks, error } = await supabase
    .from('tasks')
    .select(`
      id,
      spec_id,
      name,
      status,
      priority,
      repo_id,
      assigned_to,
      branch,
      pr_url,
      deps,
      touches,
      done_when,
      created_at,
      updated_at,
      started_at,
      completed_at,
      repos(repo_id, name)
    `)
    .eq('assigned_to', session.user_id)
    .neq('status', 'done')
    .order('priority', { ascending: false })

  if (error) {
    return NextResponse.json({ error: error.message }, { status: 500 })
  }

  // Transform to CLI format
  const formatted = tasks?.map(t => ({
    id: t.id,
    specId: t.spec_id,
    name: t.name,
    status: t.status,
    priority: t.priority,
    repo: (t.repos as any)?.repo_id || null,
    assignedTo: t.assigned_to,
    branch: t.branch,
    pr: t.pr_url,
    deps: t.deps,
    touches: t.touches,
    createdAt: t.created_at,
    updatedAt: t.updated_at,
    startedAt: t.started_at,
    completedAt: t.completed_at,
  }))

  return NextResponse.json(formatted || [])
}
