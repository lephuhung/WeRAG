import { CreateChat } from "@/components/create-chat";

export default async function KbCreateChatPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <CreateChat kbId={id} />;
}
