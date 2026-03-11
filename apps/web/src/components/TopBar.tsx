import { Link } from "react-router-dom";
import { useAuthActions, useAuthState } from "@/contexts/AuthContext";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Search, Settings, User } from "lucide-react";

export function TopBar() {
  const { user } = useAuthState();
  const { logout } = useAuthActions();

  return (
    <header className="sticky top-0 z-40 flex h-14 shrink-0 items-center gap-4 border-b border-[var(--plum-border)] bg-[var(--plum-panel)]/80 px-4 backdrop-blur-md">
      <Link
        to="/"
        className="flex items-center gap-2.5 rounded-[var(--radius-md)] transition-opacity hover:opacity-90"
        aria-label="Plum home"
      >
        <div
          className="size-8 rounded-full bg-[var(--plum-accent)] shadow-[0_0_20px_var(--plum-accent-soft)]"
          aria-hidden
        />
        <span
          className="text-lg font-semibold tracking-tight text-[var(--plum-text)]"
          style={{ fontFamily: "var(--font-display)" }}
        >
          Plum
        </span>
      </Link>

      <div className="flex flex-1 justify-center px-4">
        <div className="relative w-full max-w-md">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--plum-muted)]" />
          <Input
            type="search"
            placeholder="Search…"
            className="h-9 pl-9 bg-[var(--plum-bg)]/60 border-[var(--plum-border)]"
          />
        </div>
      </div>

      <div className="flex items-center gap-1">
        <Link to="/settings">
          <Button variant="icon" size="icon" aria-label="Settings">
            <Settings className="size-5" />
          </Button>
        </Link>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="icon" size="icon" aria-label="Profile">
              <User className="size-5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            {user?.email && (
              <div className="px-2 py-1.5 text-sm text-[var(--plum-muted)] truncate">
                {user.email}
              </div>
            )}
            <DropdownMenuItem
              onSelect={() => logout()}
              className="text-[var(--plum-accent)] focus:text-[var(--plum-accent)]"
            >
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
