'use client'

import { useEffect, useState } from 'react'
import { createClient } from '@/lib/supabase/client'
import { Database } from '@/lib/database.types'
import { cn } from '@/lib/utils'

type Task = Database['public']['Tables']['tasks']['Row']
type Spec = Database['public']['Tables']['specs']['Row']

type TaskWithSpec = Task & { specs: Spec }

const LANES = [
  { id: 'backlog', label: 'Backlog', color: 'bg-gray-500' },
  { id: 'in_progress', label: 'In Progress', color: 'bg-blue-500' },
  { id: 'in_review', label: 'In Review', color: 'bg-yellow-500' },
  { id: 'done', label: 'Done', color: 'bg-green-500' },
] as const

export function Board({ projectId }: { projectId: string }) {
  const [tasks, setTasks] = useState<TaskWithSpec[]>([])
  const [loading, setLoading] = useState(true)
  const supabase = createClient()

  useEffect(() => {
    loadTasks()

    // Subscribe to realtime updates
    const channel = supabase
      .channel('tasks')
      .on(
        'postgres_changes',
        {
          event: '*',
          schema: 'public',
          table: 'tasks',
        },
        () => {
          loadTasks()
        }
      )
      .subscribe()

    return () => {
      supabase.removeChannel(channel)
    }
  }, [projectId])

  async function loadTasks() {
    const { data } = await supabase
      .from('tasks')
      .select('*, specs!inner(*)')
      .eq('specs.project_id', projectId)
      .order('priority', { ascending: false })

    setTasks((data as TaskWithSpec[]) || [])
    setLoading(false)
  }

  async function updateTaskStatus(taskId: string, status: Task['status']) {
    await supabase
      .from('tasks')
      .update({
        status,
        started_at: status === 'in_progress' ? new Date().toISOString() : undefined,
        completed_at: status === 'done' ? new Date().toISOString() : undefined,
      })
      .eq('id', taskId)
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-muted-foreground">Loading board...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Summary */}
      <div className="flex gap-4">
        <Stat label="Total" value={tasks.length} />
        <Stat label="In Progress" value={tasks.filter(t => t.status === 'in_progress').length} />
        <Stat label="Done" value={tasks.filter(t => t.status === 'done').length} />
        <ProgressBar tasks={tasks} />
      </div>

      {/* Kanban Board */}
      <div className="grid grid-cols-4 gap-4">
        {LANES.map((lane) => (
          <Lane
            key={lane.id}
            lane={lane}
            tasks={tasks.filter((t) => t.status === lane.id)}
            onStatusChange={updateTaskStatus}
          />
        ))}
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="px-4 py-2 bg-card border border-border rounded-lg">
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className="text-2xl font-bold">{value}</p>
    </div>
  )
}

function ProgressBar({ tasks }: { tasks: TaskWithSpec[] }) {
  const done = tasks.filter(t => t.status === 'done').length
  const total = tasks.length
  const pct = total > 0 ? Math.round((done / total) * 100) : 0

  return (
    <div className="flex-1 px-4 py-2 bg-card border border-border rounded-lg">
      <div className="flex justify-between text-sm mb-1">
        <span className="text-muted-foreground">Progress</span>
        <span className="font-medium">{pct}%</span>
      </div>
      <div className="h-2 bg-secondary rounded-full overflow-hidden">
        <div
          className="h-full bg-green-500 transition-all"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

function Lane({
  lane,
  tasks,
  onStatusChange,
}: {
  lane: typeof LANES[number]
  tasks: TaskWithSpec[]
  onStatusChange: (taskId: string, status: Task['status']) => void
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <div className={cn('w-2 h-2 rounded-full', lane.color)} />
        <h3 className="font-medium">{lane.label}</h3>
        <span className="text-sm text-muted-foreground">({tasks.length})</span>
      </div>

      <div className="space-y-2 min-h-[200px] p-2 bg-secondary/50 rounded-lg">
        {tasks.map((task) => (
          <TaskCard
            key={task.id}
            task={task}
            onStatusChange={onStatusChange}
          />
        ))}
        {tasks.length === 0 && (
          <p className="text-sm text-muted-foreground text-center py-8">
            No tasks
          </p>
        )}
      </div>
    </div>
  )
}

function TaskCard({
  task,
  onStatusChange,
}: {
  task: TaskWithSpec
  onStatusChange: (taskId: string, status: Task['status']) => void
}) {
  const [open, setOpen] = useState(false)

  return (
    <div className="p-3 bg-card border border-border rounded-lg space-y-2">
      <div className="flex items-start justify-between gap-2">
        <h4 className="font-medium text-sm leading-tight">{task.name}</h4>
        <button
          onClick={() => setOpen(!open)}
          className="text-muted-foreground hover:text-foreground"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </div>

      <p className="text-xs text-muted-foreground truncate">
        {task.specs.title}
      </p>

      {open && (
        <div className="pt-2 border-t border-border space-y-2">
          {task.assigned_to && (
            <p className="text-xs text-muted-foreground">
              Assigned: {task.assigned_to.slice(0, 8)}...
            </p>
          )}
          {task.branch && (
            <p className="text-xs text-muted-foreground">
              Branch: {task.branch}
            </p>
          )}
          {task.pr_url && (
            <a
              href={task.pr_url}
              target="_blank"
              className="text-xs text-blue-400 hover:underline"
            >
              View PR
            </a>
          )}

          {/* Quick status buttons */}
          <div className="flex gap-1 pt-1">
            {LANES.map((lane) => (
              <button
                key={lane.id}
                onClick={() => onStatusChange(task.id, lane.id)}
                disabled={task.status === lane.id}
                className={cn(
                  'flex-1 py-1 text-xs rounded transition',
                  task.status === lane.id
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-secondary hover:bg-secondary/80'
                )}
              >
                {lane.label.split(' ')[0]}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
