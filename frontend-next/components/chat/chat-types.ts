/* The shape of a row in the conversation. The message components
 * (components/chat/chat-messages.tsx) and the chat screen
 * (components/chat-client.tsx) share it — it lives here so neither file
 * has to import from the other. */
import type { ArtifactMeta, MessageFeedback, TypoCorrection } from "@/lib/api/chat";
import type { AgentStepItem } from "@/components/chat/agent-steps";
import type { KnowledgeReferenceItem } from "@/components/chat/references-drawer";
import type { ChoiceList } from "@/lib/choice-list";
import type { LegalRequestCard } from "@/lib/legal-request-card";
import type { SavedMemory } from "@/components/chat/saved-memory-notice";
import type { PeopleRecord } from "@/components/chat/people-card";
import type { DocumentSelection } from "@/lib/api/document-workspace";
import type { ScopeClarification } from "@/lib/document-scope";
import type { Illustration, RewriteProposal } from "@/lib/api/document-ops";

export type UiAttachment = { name: string; size?: number; title?: string; isImage?: boolean };

export type UiMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  /** When the row was saved (history) or sent (this client) — ISO time. */
  createdAt?: string;
  thinking?: string;
  /** Ordered agent timeline (thinking rounds + tool calls), rendered by
   * ThinkingDisplay — mirrors the Vue smart-agent event stream. */
  steps?: AgentStepItem[];
  agentDurationMs?: number;
  streaming?: boolean;
  isError?: boolean;
  /** The error of this turn, shown once in the bubble (see ErrorNote). */
  errorText?: string;
  assistantMessageId?: string;
  references?: KnowledgeReferenceItem[];
  abbreviationCandidates?: string[];
  /** Memories the assistant stored on its own since the last turn ("I just saved …"). */
  savedMemories?: SavedMemory[];
  /** Document assistant: the scope clarification card this turn answered with. */
  scopeCard?: ScopeClarification;
  /** Document assistant: the options (which document, which meaning) this turn answered with. */
  choiceList?: ChoiceList;
  /** A legal document the knowledge base has no full text of (requested / rejected / expired). */
  legalRequestCard?: LegalRequestCard;
  /** Document assistant: rewrite proposals of this turn (applied with a button). */
  rewriteProposals?: UiRewriteProposal[];
  /** Document assistant: diagrams / pictures proposed this turn. */
  illustrations?: UiIllustration[];
  peopleData?: PeopleRecord[];
  attachments?: UiAttachment[];
  /** User turn: editor passage sent with it (document assistant). */
  document_selection?: DocumentSelection;
  /** User turn: words the gate read without asking (cascc → các). */
  autoCorrections?: TypoCorrection[];
  /** User turn: file names of the documents named with @ (document assistant). */
  document_mentions?: string[];
  /** Tool/skill-generated files of this turn — images render inline. */
  artifacts?: ArtifactMeta[];
  /** Owner's rating of this answer as loaded from history. */
  feedback?: MessageFeedback;
  feedbackComment?: string;
  /** History `is_completed`, or set when a live turn ends via a complete
   * event — gates the suggestions fetch (Vue: message.is_completed). */
  isCompleted?: boolean;
  /** The turn finished streaming in this client → suggestions POST ensure
   * instead of read-only GET (Vue: onTurnComplete → ensure=true). */
  suggestionEnsure?: boolean;
  /** Earlier conversation was summarized up to this turn (stored context
   * checkpoint) — a marker that survives a reload. */
  contextCompacted?: { summary: string; degraded?: boolean };
};

export type UiRewriteProposal = RewriteProposal & { appliedVariantId?: string };
export type UiIllustration = Illustration & { applied?: boolean };
