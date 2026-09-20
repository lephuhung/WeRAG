import { Header } from "@/components/header";
import { RequireAuth } from "@/components/require-auth";
import { Sidebar } from "@/components/sidebar";
import { AuthProvider } from "@/lib/auth";
import { I18nProvider } from "@/lib/i18n";

export default function PlatformLayout({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <AuthProvider>
        <RequireAuth>
          <div className="flex h-screen min-h-0 w-full bg-canvas">
            <Sidebar />
            <div className="flex min-w-0 flex-1 flex-col">
              <Header />
              <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
            </div>
          </div>
        </RequireAuth>
      </AuthProvider>
    </I18nProvider>
  );
}
