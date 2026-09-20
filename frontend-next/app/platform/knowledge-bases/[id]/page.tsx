import { KbDetail } from "@/components/kb-detail";

export default async function KnowledgeBaseDetail({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <KbDetail kbId={id} />;
}
