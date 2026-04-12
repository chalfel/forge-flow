export type Json =
  | string
  | number
  | boolean
  | null
  | { [key: string]: Json | undefined }
  | Json[]

export type Database = {
  public: {
    Tables: {
      teams: {
        Row: {
          id: string
          name: string
          slug: string
          created_at: string
          updated_at: string
        }
        Insert: {
          id?: string
          name: string
          slug: string
          created_at?: string
          updated_at?: string
        }
        Update: {
          id?: string
          name?: string
          slug?: string
          created_at?: string
          updated_at?: string
        }
      }
      team_members: {
        Row: {
          id: string
          team_id: string
          user_id: string
          role: 'owner' | 'admin' | 'member'
          created_at: string
        }
        Insert: {
          id?: string
          team_id: string
          user_id: string
          role?: 'owner' | 'admin' | 'member'
          created_at?: string
        }
        Update: {
          id?: string
          team_id?: string
          user_id?: string
          role?: 'owner' | 'admin' | 'member'
          created_at?: string
        }
      }
      projects: {
        Row: {
          id: string
          team_id: string
          name: string
          slug: string
          description: string | null
          created_at: string
          updated_at: string
        }
        Insert: {
          id?: string
          team_id: string
          name: string
          slug: string
          description?: string | null
          created_at?: string
          updated_at?: string
        }
        Update: {
          id?: string
          team_id?: string
          name?: string
          slug?: string
          description?: string | null
          created_at?: string
          updated_at?: string
        }
      }
      repos: {
        Row: {
          id: string
          project_id: string
          repo_id: string
          name: string
          git_url: string | null
          role: string | null
          created_at: string
        }
        Insert: {
          id?: string
          project_id: string
          repo_id: string
          name: string
          git_url?: string | null
          role?: string | null
          created_at?: string
        }
        Update: {
          id?: string
          project_id?: string
          repo_id?: string
          name?: string
          git_url?: string | null
          role?: string | null
          created_at?: string
        }
      }
      knowledge_bases: {
        Row: {
          id: string
          project_id: string
          constraints: string | null
          memory: string | null
          architecture: string | null
          business: string | null
          conventions: string | null
          updated_at: string
        }
        Insert: {
          id?: string
          project_id: string
          constraints?: string | null
          memory?: string | null
          architecture?: string | null
          business?: string | null
          conventions?: string | null
          updated_at?: string
        }
        Update: {
          id?: string
          project_id?: string
          constraints?: string | null
          memory?: string | null
          architecture?: string | null
          business?: string | null
          conventions?: string | null
          updated_at?: string
        }
      }
      specs: {
        Row: {
          id: string
          project_id: string
          title: string
          branch: string | null
          priority: 'p0' | 'p1' | 'p2' | 'p3' | null
          status: 'draft' | 'active' | 'completed' | 'archived'
          content: string
          created_at: string
          updated_at: string
        }
        Insert: {
          id?: string
          project_id: string
          title: string
          branch?: string | null
          priority?: 'p0' | 'p1' | 'p2' | 'p3' | null
          status?: 'draft' | 'active' | 'completed' | 'archived'
          content: string
          created_at?: string
          updated_at?: string
        }
        Update: {
          id?: string
          project_id?: string
          title?: string
          branch?: string | null
          priority?: 'p0' | 'p1' | 'p2' | 'p3' | null
          status?: 'draft' | 'active' | 'completed' | 'archived'
          content?: string
          created_at?: string
          updated_at?: string
        }
      }
      tasks: {
        Row: {
          id: string
          spec_id: string
          name: string
          status: 'backlog' | 'in_progress' | 'in_review' | 'done' | 'blocked'
          priority: number
          repo_id: string | null
          assigned_to: string | null
          branch: string | null
          pr_url: string | null
          deps: string[]
          touches: string[]
          done_when: string[]
          created_at: string
          updated_at: string
          started_at: string | null
          completed_at: string | null
        }
        Insert: {
          id?: string
          spec_id: string
          name: string
          status?: 'backlog' | 'in_progress' | 'in_review' | 'done' | 'blocked'
          priority?: number
          repo_id?: string | null
          assigned_to?: string | null
          branch?: string | null
          pr_url?: string | null
          deps?: string[]
          touches?: string[]
          done_when?: string[]
          created_at?: string
          updated_at?: string
          started_at?: string | null
          completed_at?: string | null
        }
        Update: {
          id?: string
          spec_id?: string
          name?: string
          status?: 'backlog' | 'in_progress' | 'in_review' | 'done' | 'blocked'
          priority?: number
          repo_id?: string | null
          assigned_to?: string | null
          branch?: string | null
          pr_url?: string | null
          deps?: string[]
          touches?: string[]
          done_when?: string[]
          created_at?: string
          updated_at?: string
          started_at?: string | null
          completed_at?: string | null
        }
      }
      task_events: {
        Row: {
          id: string
          task_id: string
          type: 'progress' | 'decision' | 'blocked' | 'need_info' | 'handoff'
          message: string
          details: Json | null
          created_at: string
        }
        Insert: {
          id?: string
          task_id: string
          type: 'progress' | 'decision' | 'blocked' | 'need_info' | 'handoff'
          message: string
          details?: Json | null
          created_at?: string
        }
        Update: {
          id?: string
          task_id?: string
          type?: 'progress' | 'decision' | 'blocked' | 'need_info' | 'handoff'
          message?: string
          details?: Json | null
          created_at?: string
        }
      }
      profiles: {
        Row: {
          id: string
          email: string | null
          name: string | null
          avatar_url: string | null
          created_at: string
          updated_at: string
        }
        Insert: {
          id: string
          email?: string | null
          name?: string | null
          avatar_url?: string | null
          created_at?: string
          updated_at?: string
        }
        Update: {
          id?: string
          email?: string | null
          name?: string | null
          avatar_url?: string | null
          created_at?: string
          updated_at?: string
        }
      }
      device_codes: {
        Row: {
          id: string
          device_code: string
          user_code: string
          user_id: string | null
          status: 'pending' | 'authorized' | 'expired'
          expires_at: string
          last_polled_at: string | null
          created_at: string
        }
        Insert: {
          id?: string
          device_code: string
          user_code: string
          user_id?: string | null
          status?: 'pending' | 'authorized' | 'expired'
          expires_at: string
          last_polled_at?: string | null
          created_at?: string
        }
        Update: {
          id?: string
          device_code?: string
          user_code?: string
          user_id?: string | null
          status?: 'pending' | 'authorized' | 'expired'
          expires_at?: string
          last_polled_at?: string | null
          created_at?: string
        }
      }
    }
    Enums: {
      spec_status: 'draft' | 'active' | 'completed' | 'archived'
      task_status: 'backlog' | 'in_progress' | 'in_review' | 'done' | 'blocked'
    }
  }
}
