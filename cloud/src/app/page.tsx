import Link from 'next/link'

export default function Home() {
  return (
    <main className="min-h-screen flex flex-col items-center justify-center p-8">
      <div className="max-w-2xl text-center space-y-8">
        <h1 className="text-5xl font-bold tracking-tight">
          Forge Flow
        </h1>
        <p className="text-xl text-muted-foreground">
          Strategy-driven AI task orchestration for tech teams.
          <br />
          Turn your roadmap into executed code.
        </p>
        <div className="flex gap-4 justify-center">
          <Link
            href="/login"
            className="px-6 py-3 bg-primary text-primary-foreground rounded-lg font-medium hover:opacity-90 transition"
          >
            Get Started
          </Link>
          <Link
            href="/docs"
            className="px-6 py-3 border border-border rounded-lg font-medium hover:bg-secondary transition"
          >
            Documentation
          </Link>
        </div>
      </div>
    </main>
  )
}
