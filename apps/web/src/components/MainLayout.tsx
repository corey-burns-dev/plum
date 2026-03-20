import { Outlet } from "react-router-dom";
import { usePlayer } from "@/contexts/PlayerContext";
import { PlaybackDock } from "./PlaybackDock";
import { TopBar } from "./TopBar";
import { Sidebar } from "./Sidebar";

export function MainLayout() {
  const { activeMode, isDockOpen, viewMode } = usePlayer();
  const reserveDockSpace = isDockOpen && activeMode === "music" && viewMode === "docked";

  return (
    <div className="flex h-screen overflow-hidden flex-col">
      <TopBar />
      <div className="flex flex-1 min-h-0">
        <Sidebar />
        <main className="flex min-w-0 flex-1 flex-col">
          <section
            className={`main-content flex-1 overflow-auto p-4 md:p-6 ${reserveDockSpace ? "main-content--with-dock" : ""}`}
          >
            <Outlet />
          </section>
        </main>
      </div>
      <PlaybackDock />
    </div>
  );
}
