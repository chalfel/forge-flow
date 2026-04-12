import { NextRequest, NextResponse } from 'next/server'
import { createServiceClient } from '@/lib/supabase/server'

// GET /api/projects/[id]/knowledge
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

  // Get knowledge base
  const { data: kb, error } = await supabase
    .from('knowledge_bases')
    .select('*')
    .eq('project_id', params.id)
    .single()

  if (error || !kb) {
    // Return empty KB if none exists
    return NextResponse.json({
      projectId: params.id,
      constraints: null,
      memory: null,
      architecture: null,
      business: null,
      conventions: null,
      updatedAt: null,
    })
  }

  return NextResponse.json({
    projectId: kb.project_id,
    constraints: kb.constraints,
    memory: kb.memory,
    architecture: kb.architecture,
    business: kb.business,
    conventions: kb.conventions,
    updatedAt: kb.updated_at,
  })
}
