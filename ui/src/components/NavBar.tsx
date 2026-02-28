"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

export function NavBar() {
  const pathname = usePathname();
  const links = [
    { href: "/", label: "Dashboard" },
    { href: "/search", label: "Search" },
    { href: "/ask", label: "Ask" },
  ];
  return (
    <nav className="border-b border-zinc-200 dark:border-zinc-800 px-4 py-3 flex gap-4">
      {links.map(({ href, label }) => (
        <Link
          key={href}
          href={href}
          className={`font-medium hover:underline ${pathname === href ? "underline" : ""}`}
          aria-current={pathname === href ? "page" : undefined}
        >
          {label}
        </Link>
      ))}
    </nav>
  );
}
