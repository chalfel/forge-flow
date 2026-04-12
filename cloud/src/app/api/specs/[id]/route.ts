import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'

// GET /api/specs/[id]
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

  // Get spec with tasks
  const { data: spec, error } = await supabase
    .from('specs')
    .select(`
      *,
      tasks(*)
    `)
    .eq('id', params.id)
    .single()

  if (error || !spec) {
    return NextResponse.json({ error: 'not found' }, { status: 404 })
  }

  return NextResponse.json({
    id: spec.id,
    projectId: spec.project_id,
    title: spec.title,
    branch: spec.branch,
    priority: spec.priority,
    status: spec.status,
    content: spec.content,
    tasks: spec.tasks,
    createdAt: spec.created_at,
    updatedAt: spec.updated_at,
  })
}
