-- ============================================================================
-- Teams & Projects
-- ============================================================================

create table teams (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  slug text unique not null,
  created_at timestamptz default now(),
  updated_at timestamptz default now()
);

create table team_members (
  id uuid primary key default gen_random_uuid(),
  team_id uuid references teams(id) on delete cascade not null,
  user_id uuid references auth.users(id) on delete cascade not null,
  role text default 'member' check (role in ('owner', 'admin', 'member')),
  created_at timestamptz default now(),
  unique(team_id, user_id)
);

create table projects (
  id uuid primary key default gen_random_uuid(),
  team_id uuid references teams(id) on delete cascade not null,
  name text not null,
  slug text not null,
  description text,
  created_at timestamptz default now(),
  updated_at timestamptz default now(),
  unique(team_id, slug)
);

create table repos (
  id uuid primary key default gen_random_uuid(),
  project_id uuid references projects(id) on delete cascade not null,
  repo_id text not null, -- logical ID used in specs
  name text not null,
  git_url text,
  role text, -- frontend, backend, shared, etc.
  created_at timestamptz default now(),
  unique(project_id, repo_id)
);

-- ============================================================================
-- Knowledge Base
-- ============================================================================

create table knowledge_bases (
  id uuid primary key default gen_random_uuid(),
  project_id uuid references projects(id) on delete cascade unique not null,
  constraints text,
  memory text,
  architecture text,
  business text,
  conventions text,
  updated_at timestamptz default now()
);

-- ============================================================================
-- Specs & Tasks
-- ============================================================================

create type spec_status as enum ('draft', 'active', 'completed', 'archived');
create type task_status as enum ('backlog', 'in_progress', 'in_review', 'done', 'blocked');

create table specs (
  id uuid primary key default gen_random_uuid(),
  project_id uuid references projects(id) on delete cascade not null,
  title text not null,
  branch text,
  priority text check (priority in ('p0', 'p1', 'p2', 'p3')),
  status spec_status default 'draft',
  content text not null,
  created_at timestamptz default now(),
  updated_at timestamptz default now()
);

create table tasks (
  id uuid primary key default gen_random_uuid(),
  spec_id uuid references specs(id) on delete cascade not null,
  name text not null,
  status task_status default 'backlog',
  priority int default 0,
  repo_id uuid references repos(id),
  assigned_to uuid references auth.users(id),
  branch text,
  pr_url text,
  deps text[] default '{}',
  touches text[] default '{}',
  done_when text[] default '{}',
  created_at timestamptz default now(),
  updated_at timestamptz default now(),
  started_at timestamptz,
  completed_at timestamptz
);

create table task_events (
  id uuid primary key default gen_random_uuid(),
  task_id uuid references tasks(id) on delete cascade not null,
  type text not null check (type in ('progress', 'decision', 'blocked', 'need_info', 'handoff')),
  message text not null,
  details jsonb,
  created_at timestamptz default now()
);

create table task_attempts (
  id uuid primary key default gen_random_uuid(),
  task_id uuid references tasks(id) on delete cascade not null,
  attempt_number int not null,
  status text check (status in ('running', 'succeeded', 'failed')),
  error text,
  review_feedback jsonb,
  started_at timestamptz default now(),
  ended_at timestamptz
);

-- ============================================================================
-- Device Code Auth (for CLI)
-- ============================================================================

create table device_codes (
  id uuid primary key default gen_random_uuid(),
  device_code text unique not null,
  user_code text unique not null,
  user_id uuid references auth.users(id),
  status text default 'pending' check (status in ('pending', 'authorized', 'expired')),
  expires_at timestamptz not null,
  last_polled_at timestamptz,
  created_at timestamptz default now()
);

-- ============================================================================
-- User Profiles (extends auth.users)
-- ============================================================================

create table profiles (
  id uuid primary key references auth.users(id) on delete cascade,
  email text,
  name text,
  avatar_url text,
  created_at timestamptz default now(),
  updated_at timestamptz default now()
);

-- ============================================================================
-- Indexes
-- ============================================================================

create index idx_team_members_user on team_members(user_id);
create index idx_projects_team on projects(team_id);
create index idx_specs_project on specs(project_id);
create index idx_tasks_spec on tasks(spec_id);
create index idx_tasks_assigned on tasks(assigned_to);
create index idx_tasks_status on tasks(status);
create index idx_task_events_task on task_events(task_id);
create index idx_device_codes_user_code on device_codes(user_code);

-- ============================================================================
-- Row Level Security
-- ============================================================================

alter table teams enable row level security;
alter table team_members enable row level security;
alter table projects enable row level security;
alter table repos enable row level security;
alter table knowledge_bases enable row level security;
alter table specs enable row level security;
alter table tasks enable row level security;
alter table task_events enable row level security;
alter table task_attempts enable row level security;
alter table device_codes enable row level security;
alter table profiles enable row level security;

-- Team members can see their teams
create policy "Team members can view their teams"
  on teams for select
  using (
    exists (
      select 1 from team_members
      where team_members.team_id = teams.id
      and team_members.user_id = auth.uid()
    )
  );

-- Team members can view team membership
create policy "Team members can view memberships"
  on team_members for select
  using (
    exists (
      select 1 from team_members as tm
      where tm.team_id = team_members.team_id
      and tm.user_id = auth.uid()
    )
  );

-- Team members can view projects
create policy "Team members can view projects"
  on projects for select
  using (
    exists (
      select 1 from team_members
      where team_members.team_id = projects.team_id
      and team_members.user_id = auth.uid()
    )
  );

-- Team members can view specs
create policy "Team members can view specs"
  on specs for select
  using (
    exists (
      select 1 from projects
      join team_members on team_members.team_id = projects.team_id
      where projects.id = specs.project_id
      and team_members.user_id = auth.uid()
    )
  );

-- Team members can view tasks
create policy "Team members can view tasks"
  on tasks for select
  using (
    exists (
      select 1 from specs
      join projects on projects.id = specs.project_id
      join team_members on team_members.team_id = projects.team_id
      where specs.id = tasks.spec_id
      and team_members.user_id = auth.uid()
    )
  );

-- Users can update their assigned tasks
create policy "Users can update assigned tasks"
  on tasks for update
  using (assigned_to = auth.uid());

-- Users can view their profile
create policy "Users can view own profile"
  on profiles for select
  using (id = auth.uid());

-- Users can update their profile
create policy "Users can update own profile"
  on profiles for update
  using (id = auth.uid());

-- ============================================================================
-- Functions & Triggers
-- ============================================================================

-- Auto-create profile on signup
create or replace function handle_new_user()
returns trigger as $$
begin
  insert into public.profiles (id, email, name)
  values (new.id, new.email, new.raw_user_meta_data->>'name');
  return new;
end;
$$ language plpgsql security definer;

create trigger on_auth_user_created
  after insert on auth.users
  for each row execute procedure handle_new_user();

-- Update timestamps
create or replace function update_updated_at()
returns trigger as $$
begin
  new.updated_at = now();
  return new;
end;
$$ language plpgsql;

create trigger update_teams_updated_at before update on teams
  for each row execute procedure update_updated_at();
create trigger update_projects_updated_at before update on projects
  for each row execute procedure update_updated_at();
create trigger update_specs_updated_at before update on specs
  for each row execute procedure update_updated_at();
create trigger update_tasks_updated_at before update on tasks
  for each row execute procedure update_updated_at();
create trigger update_profiles_updated_at before update on profiles
  for each row execute procedure update_updated_at();

-- ============================================================================
-- Realtime
-- ============================================================================

-- Enable realtime for tasks (for live board updates)
alter publication supabase_realtime add table tasks;
alter publication supabase_realtime add table task_events;
