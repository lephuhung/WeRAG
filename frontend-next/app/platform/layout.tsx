/* Everything a signed-in page needs, with no chrome of its own.
 *
 * The chrome lives one level down, in two route groups that share these
 * providers but not each other's frame:
 *   (app)     — the working app: chat rail, header, plan banner
 *   (console) — the system administration console, its own full page
 * Route groups add no URL segment, so the paths stay /platform/...
 * The console used to sit inside the app frame, which meant the chat list
 * and the console's own section rail were two menus nested in one page
 * (reported 2026-11-16: "2 menu lồng nhau quá chật").
 */
import { RequireAuth } from "@/components/require-auth";
import { AuthProvider } from "@/lib/auth";
import { CommandPaletteProvider } from "@/components/command-palette/command-palette-context";
import { GlobalCommandPalette } from "@/components/command-palette/global-command-palette";
import { UploadTasksProvider } from "@/lib/upload-tasks";
import { UploadTasksPanel } from "@/components/upload-tasks/upload-tasks-panel";
import { GlobalFileDrop } from "@/components/upload-tasks/global-file-drop";
import { ToastProvider } from "@/components/toast";
import { ConfirmProvider } from "@/components/confirm-dialog";
import { WorkContextPrompt } from "@/components/work-context-prompt";
import { SessionActivityWatch } from "@/components/session-activity-watch";

export default function PlatformLayout({ children }: { children: React.ReactNode }) {
  return (
    /* The i18n provider lives in the root layout — every route shares it. */
    <AuthProvider>
      <RequireAuth>
        <ToastProvider>
          <ConfirmProvider>
            <CommandPaletteProvider>
              <UploadTasksProvider>
                {children}
                <GlobalCommandPalette />
                <UploadTasksPanel />
                <GlobalFileDrop />
                <WorkContextPrompt />
                <SessionActivityWatch />
              </UploadTasksProvider>
            </CommandPaletteProvider>
          </ConfirmProvider>
        </ToastProvider>
      </RequireAuth>
    </AuthProvider>
  );
}
