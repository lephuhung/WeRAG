package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	// topicFuzzyThreshold is where character-bigram overlap alone is enough to
	// call two labels the same subject.
	//
	// Set high on purpose. Merging two topics that are not the same thing
	// corrupts the count that decides what becomes a memory, and it is
	// invisible when it happens. A missed merge only delays a promotion, and
	// the tier below catches most of them anyway. Graphiti holds its
	// deterministic tier at a comparable level for the same reason.
	topicFuzzyThreshold = 0.80
	// topicCandidateLimit bounds how many existing topics are shown to the
	// adjudicating model. One person's topic list is small; this is a guard
	// against a pathological account, not a normal working limit.
	topicCandidateLimit = 40
	// topicMaxAliases bounds the alias list on one topic.
	topicMaxAliases = 12
)

// topicResolution is where one surface form ended up.
type topicResolution struct {
	// Canonical is the existing topic this label belongs to, or nil when it is
	// genuinely a new subject.
	Canonical *types.MemoryTopicStat
	// Surface is what the model actually said, recorded as an alias when it
	// differs from the canonical label.
	Surface string
	// Tier records which rule decided, for logs and for tests that need to
	// assert an expensive tier was not reached.
	Tier string
	// MergedLabel is a better name for the merged subject, when the model
	// offered one and it passed the guard against generalising. Empty means
	// keep the label the subject already has.
	MergedLabel string
}

// resolveTopics maps the labels one extraction run produced onto the subjects
// this person already has.
//
// The problem this solves is that a model asked to name a topic will not name
// it the same way twice: "门店排班管理" one run, "店员班次安排" the
// next. Treating the string as an identity means the same subject is counted
// under several keys and never reaches the promotion threshold — the feature
// looks enabled and learns nothing.
//
// The fix is the one both mem0 and Graphiti converged on: never trust the
// surface string, resolve it against what already exists, cheapest test first.
//
//	tier 1  normalised equality, including previously recorded aliases
//	tier 2  character-bigram overlap, gated so short labels do not match loosely
//	tier 3  one batched model call over the remaining labels
//
// Tier 3 is the only one that costs anything, and it is usually skipped: the
// extraction prompt already shows the model this person's existing topics and
// asks it to reuse a label verbatim, so most runs resolve at tier 1.
func (s *Service) resolveTopics(
	ctx context.Context,
	scope interfaces.MemoryScope,
	modelID string,
	surfaces []string,
) []topicResolution {
	if len(surfaces) == 0 {
		return nil
	}
	existing, err := s.repo.TopTopics(ctx, scope, topicCandidateLimit)
	if err != nil {
		logger.Warnf(ctx, "memory: load existing topics failed: %v", err)
		existing = nil
	}

	resolutions := make([]topicResolution, 0, len(surfaces))
	var unresolved []int

	for _, surface := range surfaces {
		resolution := topicResolution{Surface: surface}
		if match := matchTopicExactly(surface, existing); match != nil {
			resolution.Canonical = match
			// Distinguish "the extraction model echoed a tracked label" from
			// "the resolver's own normalisation matched". Both look like an
			// exact hit here, but only the first is a judgement the model made
			// — and reporting that as the cheapest, most certain tier is how an
			// over-merge hides.
			if surface == match.Topic {
				resolution.Tier = "reused"
			} else {
				resolution.Tier = "exact"
			}
		} else if match := matchTopicLoosely(surface, existing); match != nil {
			resolution.Canonical = match
			resolution.Tier = "fuzzy"
		} else {
			unresolved = append(unresolved, len(resolutions))
		}
		resolutions = append(resolutions, resolution)
	}

	if len(unresolved) > 0 && len(existing) > 0 {
		s.adjudicateTopics(ctx, modelID, existing, resolutions, unresolved)
	}

	// Two labels in the same run can be the same new subject. Without this the
	// run creates two rows that every later run then has to keep apart.
	collapseNewTopicsWithinRun(resolutions)

	return resolutions
}

// matchTopicExactly is tier 1: the normalised label, or any wording that has
// already been resolved to this topic before.
func matchTopicExactly(surface string, existing []*types.MemoryTopicStat) *types.MemoryTopicStat {
	key := types.NormalizeTopicKey(surface)
	if key == "" {
		return nil
	}
	for _, stat := range existing {
		if stat == nil {
			continue
		}
		if stat.NormalizedKey == key || stat.Aliases.Has(surface) {
			return stat
		}
	}
	return nil
}

// matchTopicLoosely is tier 2: high character-bigram overlap, and only for
// labels specific enough that the overlap means something.
func matchTopicLoosely(surface string, existing []*types.MemoryTopicStat) *types.MemoryTopicStat {
	if !types.TopicIsSpecificEnoughToMatchLoosely(surface) {
		return nil
	}
	var (
		best      *types.MemoryTopicStat
		bestScore float64
	)
	for _, stat := range existing {
		if stat == nil || !types.TopicIsSpecificEnoughToMatchLoosely(stat.Topic) {
			continue
		}
		score := types.TopicSimilarity(surface, stat.Topic)
		if score > bestScore {
			best, bestScore = stat, score
		}
	}
	if bestScore < topicFuzzyThreshold {
		return nil
	}
	return best
}

const topicAdjudicationPrompt = `You maintain the list of subjects one person keeps asking about. Below are the "Tracked subjects" and the "New labels".

For each new label, decide whether it and one tracked subject are **the same thing** — the same thing, not merely related.

When they are the same and one name is clearly more complete or precise, give the name to keep in "label":
- "TTHC" and "thủ tục hành chính" → label the full form "thủ tục hành chính".
- "cấp phép xây dựng" and "cấp phép xây dựng nhà ở" → label the more specific one.
- When both names are about as good, leave "label" empty.
- **Never** give a broader name ("pháp luật", "văn bản", "quản lý chung"), and never join two names
  ("A và B"). A name may only get more precise, never more general — otherwise every merge widens
  the subject a little, until it is a bucket that holds everything.
- Write the label in the language of the labels themselves (Vietnamese stays Vietnamese); never translate it.

Same thing:
- Synonyms, rewording, or the same thing in more or less detail: "xử phạt vi phạm giao thông" and "phạt lỗi giao thông".
- An unimportant qualifier added: "cấp phép xây dựng" and "vấn đề cấp phép xây dựng".

Not the same thing:
- Different problems in one area: "xử phạt vi phạm giao thông" and "đăng ký xe".
- One is a **specific lookup** inside the other's area: tracked "cấp phép xây dựng", new label
  "hồ sơ cấp phép nhà số 5 Lê Lợi" — it does belong to that area, but it is one specific query,
  not the same standing interest. Judge these as different.
- One is a narrower concept than the other: "pháp luật đất đai" and "thu hồi đất".

When unsure, judge them different. A wrong merge mixes the counts of two subjects and can never be
seen afterwards; a missed merge only leaves one extra row for now.

Output JSON only:
{"resolutions":[{"index":<new label number>,"same_as":<tracked subject number, or null>,"label":<better name, or null>}]}`

var topicAdjudicationSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "resolutions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "index": {"type": "integer"},
          "same_as": {"type": ["integer", "null"]},
          "label": {"type": ["string", "null"]}
        },
        "required": ["index", "same_as"]
      }
    }
  },
  "required": ["resolutions"]
}`)

// adjudicateTopics is tier 3: ask the model whether the labels nothing matched
// are really new subjects.
//
// It runs once per extraction run over every unresolved label at once, rather
// than once per label, because the cost that matters here is the round trip and
// the decision is the same shape for all of them.
func (s *Service) adjudicateTopics(
	ctx context.Context,
	modelID string,
	existing []*types.MemoryTopicStat,
	resolutions []topicResolution,
	unresolved []int,
) {
	if modelID == "" {
		// Nothing to fall back on. Every label here becomes its own subject,
		// which is the wrong answer but a visible one — as opposed to silently
		// skipping the tier, which is what happened while this checked the
		// configured extraction model directly: blank is the *default* and
		// means "use the conversation model", so on a default workspace the
		// model tier never ran and every rephrasing sat in its own row at one
		// hit, forever short of the promotion threshold.
		logger.Warnf(ctx, "memory: no model available to resolve %d new topics", len(unresolved))
		return
	}
	chatModel, err := s.modelService.GetChatModel(ctx, modelID)
	if err != nil || chatModel == nil {
		logger.Warnf(ctx, "memory: topic adjudication model unavailable: %v", err)
		return
	}

	var b strings.Builder
	b.WriteString("Tracked subjects:\n")
	for i, stat := range existing {
		fmt.Fprintf(&b, "[%d] %s\n", i, stat.Topic)
	}
	b.WriteString("\nNew labels:\n")
	for _, idx := range unresolved {
		fmt.Fprintf(&b, "[%d] %s\n", idx, resolutions[idx].Surface)
	}

	// Thinking off, for the reason given on completeExtraction. Silently
	// getting nothing back here would send every rephrasing to its own row.
	thinking := false
	response, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "system", Content: topicAdjudicationPrompt},
		{Role: "user", Content: b.String()},
	}, &chat.ChatOptions{
		Temperature:         0,
		MaxCompletionTokens: 800,
		Thinking:            &thinking,
		Format:              topicAdjudicationSchema,
	})
	if err != nil || response == nil {
		logger.Warnf(ctx, "memory: topic adjudication failed: %v", err)
		return
	}

	var parsed struct {
		Resolutions []struct {
			Index  int    `json:"index"`
			SameAs *int   `json:"same_as"`
			Label  string `json:"label"`
		} `json:"resolutions"`
	}
	content := strings.TrimSpace(response.Content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &parsed); err != nil {
		logger.Warnf(ctx, "memory: unparsable topic adjudication: %v", err)
		return
	}

	pending := make(map[int]struct{}, len(unresolved))
	for _, idx := range unresolved {
		pending[idx] = struct{}{}
	}
	for _, decision := range parsed.Resolutions {
		// Only labels this call was actually asked about may be reassigned. A
		// model that returns an index it was not given must not be able to
		// overwrite a match an earlier, more reliable tier already made.
		if _, ok := pending[decision.Index]; !ok {
			continue
		}
		if decision.SameAs == nil {
			continue
		}
		target := *decision.SameAs
		if target < 0 || target >= len(existing) {
			continue
		}
		resolutions[decision.Index].Canonical = existing[target]
		resolutions[decision.Index].Tier = "model"
		// A merge nothing lexical supported is the one most likely to be wrong,
		// so it is logged with both labels rather than only appearing as a
		// bumped counter on a subject the user never named.
		logger.Infof(ctx, "memory: model merged topic %q into %q",
			resolutions[decision.Index].Surface, existing[target].Topic)

		proposed := types.SanitizeMemoryTopic(decision.Label)
		if proposed == "" {
			continue
		}
		// A label translated into Chinese is not a more precise name for a
		// subject neither side wrote in Chinese.
		if types.ContainsHan(proposed) && !types.ContainsHan(existing[target].Topic) &&
			!types.ContainsHan(resolutions[decision.Index].Surface) {
			logger.Infof(ctx, "memory: rejected foreign-script label %q for %q",
				proposed, existing[target].Topic)
			continue
		}
		if !types.TopicLabelIsAnImprovement(
			existing[target].Topic, resolutions[decision.Index].Surface, proposed,
		) {
			logger.Infof(ctx, "memory: rejected proposed label %q for %q",
				proposed, existing[target].Topic)
			continue
		}
		resolutions[decision.Index].MergedLabel = proposed
	}
}

// collapseNewTopicsWithinRun points near-identical new labels from one run at
// the same surface form, so they become one row rather than two.
func collapseNewTopicsWithinRun(resolutions []topicResolution) {
	for i := range resolutions {
		if resolutions[i].Canonical != nil {
			continue
		}
		for j := 0; j < i; j++ {
			if resolutions[j].Canonical != nil {
				continue
			}
			if types.NormalizeTopicKey(resolutions[i].Surface) ==
				types.NormalizeTopicKey(resolutions[j].Surface) {
				resolutions[i].Surface = resolutions[j].Surface
				break
			}
		}
	}
}
