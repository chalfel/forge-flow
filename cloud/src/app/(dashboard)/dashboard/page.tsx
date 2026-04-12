import { redirect } from 'next/navigation'
import { createClient } from '@/lib/supabase/server'
import { Board } from '@/components/board'

export default async function DashboardPage() {
  const supabase = createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) {
    redirect('/login')
  }

  // Get user's teams
  const { data: memberships } = await supabase
    .from('team_members')
    .select('team_id, role, teams(id, name, slug)')
    .eq('user_id', user.id)

  // Get projects from first team (simplified for MVP)
  const teamId = memberships?.[0]?.team_id

  let projects: any[] = []
  if (teamId) {
    const { data } = await supabase
      .from('projects')
      .select('*')
      .eq('team_id', teamId)
    projects = data || []
  }

  return (
    <div className="min-h-screen">
      <header className="border-b border-border px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-xl font-bold">Forge Flow</h1>
          {projects.length > 0 && (
            <span className="text-muted-foreground">
              / {projects[0].name}
            </span>
          )}
        </div>
        <div className="flex items-center gap-4">
          <span className="text-sm text-muted-foreground">{user.email}</span>
          <form action="/api/auth/signout" method="POST">
            <button className="text-sm text-muted-foreground hover:text-foreground">
              Sign out
            </button>
          </form>
        </div>
      </header>

      <main className="p-6">
        {projects.length === 0 ? (
          <EmptyState />
        ) : (
          <Board projectId={projects[0].id} />
        )}
      </main>
    </div>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center min-h-[60vh] text-center">
      <h2 className="text-2xl font-bold mb-2">No projects yet</h2>
      <p className="text-muted-foreground mb-6">
        Create your first project to start orchestrating tasks.
      </p>
      <a
        href="/projects/new"
        className="px-4 py-2 bg-primary text-primary-foreground rounded-lg font-medium"
      >
        Create Project
      </a>
    </div>
  )
}
