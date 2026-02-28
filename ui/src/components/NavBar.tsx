import Link from "next/link";

export function NavBar() {
  return (
    <nav className="border-b border-zinc-200 dark:border-zinc-800 px-4 py-3 flex gap-4">
      <Link href="/" className="font-medium hover:underline">Dashboard</Link>
      <Link href="/search" className="font-medium hover:underline">Search</Link>
      <Link href="/ask" className="font-medium hover:underline">Ask</Link>
    </nav>
  );
}
