import type { Metadata } from "next";
import { Landing } from "./landing";

export const metadata: Metadata = {
  title: "weRAG — Trợ lý pháp luật",
  description:
    "Khám phá weRAG: tra cứu, hỏi đáp văn bản pháp luật có nguồn trích dẫn cho cá nhân, đội nhóm và tổ chức.",
};

export default function MarketingLandingPage() {
  return <Landing />;
}
