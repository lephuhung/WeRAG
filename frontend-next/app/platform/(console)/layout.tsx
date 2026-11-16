/* The administration console's frame: its own bar and the whole window
 * below it. No chat rail here — the console's section rail is the only
 * menu on the page. */
import { ConsoleTopBar } from "@/components/admin/console-top-bar";

export default function ConsoleLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-dvh min-h-0 w-full flex-col bg-canvas">
      <ConsoleTopBar />
      <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
    </div>
  );
}
