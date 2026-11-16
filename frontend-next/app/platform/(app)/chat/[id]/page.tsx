import { Suspense } from "react";
import { ChatClient } from "@/components/chat-client";

export default async function ChatPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <Suspense>
      <ChatClient id={id} />
    </Suspense>
  );
}
