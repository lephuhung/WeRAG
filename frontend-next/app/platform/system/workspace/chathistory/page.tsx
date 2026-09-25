"use client";

import { RequireSystemAccess } from "@/components/require-system-access";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";

export default function ChatHistoryPage() {
  return (
    <RequireSystemAccess minRole="admin">
      <div className="card p-4 sm:p-6 lg:p-8">
        <ChatHistorySettings />
      </div>
    </RequireSystemAccess>
  );
}
