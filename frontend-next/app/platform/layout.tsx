import { Header } from "@/components/header";
import { RequireAuth } from "@/components/require-auth";
import { Sidebar } from "@/components/sidebar";
import { AuthProvider } from "@/lib/auth";
import { I18nProvider } from "@/lib/i18n";
import { CommandPaletteProvider } from "@/components/command-palette/command-palette-context";
import { GlobalCommandPalette } from "@/components/command-palette/global-command-palette";
import { UploadTasksProvider } from "@/lib/upload-tasks";
import { UploadTasksPanel } from "@/components/upload-tasks/upload-tasks-panel";
import { GlobalFileDrop } from "@/components/upload-tasks/global-file-drop";

export default function PlatformLayout({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <AuthProvider>
        <RequireAuth>
          <CommandPaletteProvider>
            <UploadTasksProvider>
              <div className="flex h-dvh min-h-0 w-full bg-canvas">
                <Sidebar />
                <div className="flex min-w-0 flex-1 flex-col">
                  <Header />
                  <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
                </div>
              </div>
              <GlobalCommandPalette />
              <UploadTasksPanel />
              <GlobalFileDrop />
            </UploadTasksProvider>
          </CommandPaletteProvider>
        </RequireAuth>
      </AuthProvider>
    </I18nProvider>
  );
}

