package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Current wording for the model: a retrieved provision whose words a newer
// document replaced in place ("Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ
// đồng” tại Điều 3, Điều 4 …") is shown to the model with the new words and
// the amendment named, so it does not answer from the old figure. The
// reference the user opens keeps the stored text.

// legalPhraseChange is one phrase amendment of a retrieved chunk.
type legalPhraseChange struct {
	From, To string
	Khoan    string // "" = the whole chunk
	Amender  legalHistoryDoc
}

// LegalCurrentWording returns, by chunk id, the content of each retrieved
// chunk with the phrase amendments in force applied (pending ones noted);
// chunks without such amendments are left out.
func LegalCurrentWording(ctx context.Context, db *gorm.DB, kbIDs, chunkIDs []string, now time.Time) map[string]string {
	if db == nil || len(kbIDs) == 0 || len(chunkIDs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, legalHistoryTimeout)
	defer cancel()
	h := &legalHistory{amended: map[string]string{}, ctx: ctx, db: db.WithContext(ctx), kbIDs: kbIDs, now: now}
	chunks, err := h.loadChunks(chunkIDs)
	if err != nil {
		logger.Warnf(ctx, "[LegalWording] load chunks: %v", err)
		return nil
	}
	out := map[string]string{}
	for _, c := range chunks {
		if len(c.Legal.ArticleNos) != 1 {
			continue
		}
		changes := h.phraseChanges(c)
		if len(changes) == 0 {
			continue
		}
		if text := applyPhraseChanges(c.Content, changes, now); text != c.Content {
			out[c.ID] = text
		}
	}
	return out
}

// phraseChanges lists the phrase amendments of other documents that name
// c's Điều (and khoản) and whose replaced words c still holds, newest
// amender last.
func (h *legalHistory) phraseChanges(c legalHistoryChunk) []legalPhraseChange {
	article := c.Legal.ArticleNos[0]
	filter, _ := json.Marshal([]map[string]interface{}{{"article": article, "phrase": true}})
	var rows []legalHistoryRow
	if err := h.db.Raw(legalHistorySelect+` AND c.knowledge_id <> ?
		AND c.metadata->'legal'->'amends' @> ?::jsonb
		ORDER BY strpos(c.metadata->'legal'->>'amends', ?) > 0 DESC
		LIMIT ?`,
		h.kbIDs, c.KnowledgeID, string(filter), c.Doc.Number, legalHistoryCandidates).Scan(&rows).Error; err != nil {
		logger.Warnf(h.ctx, "[LegalWording] amending chunks: %v", err)
		return nil
	}
	khoans := map[string]bool{}
	for _, k := range c.Legal.KhoanNos {
		khoans[k] = true
	}
	content := foldSpaces(c.Content)
	var out []legalPhraseChange
	seen := map[string]bool{}
	for _, ac := range parseLegalHistoryRows(rows) {
		for _, a := range ac.Legal.Amends {
			if !a.Phrase || a.From == "" || a.Article != article {
				continue
			}
			if a.Khoan != "" && len(khoans) > 0 && !khoans[a.Khoan] {
				continue
			}
			if !strings.Contains(content, foldSpaces(a.From)) || h.amendedNumber(ac.Doc, a.Document) != c.Doc.Number {
				continue
			}
			key := ac.Doc.Number + "|" + a.Khoan + "|" + a.From
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, legalPhraseChange{From: a.From, To: a.To, Khoan: a.Khoan, Amender: ac.Doc})
		}
	}
	if len(out) == 0 {
		return nil
	}
	// an amender no longer in force changes nothing
	var numbers []string
	for _, ch := range out {
		numbers = append(numbers, ch.Amender.Number)
	}
	if expired := fullyExpiredDocs(h.ctx, h.db, numbers); len(expired) > 0 {
		kept := out[:0]
		for _, ch := range out {
			if _, gone := expired[vietnamese_legal.CatalogNumberKey(ch.Amender.Number)]; !gone {
				kept = append(kept, ch)
			}
		}
		out = kept
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Amender.EffFrom < out[j].Amender.EffFrom })
	return out
}

// applyPhraseChanges rewrites content: words replaced by an amendment in
// force read "<new> [thay cho “<old>” theo <amender>, từ <date>]"; an
// amendment not yet in force (or without a date) only notes the coming
// change after the old words.
func applyPhraseChanges(content string, changes []legalPhraseChange, now time.Time) string {
	for _, ch := range changes {
		label := ch.Amender.label()
		from, to, when := ch.From, ch.To, ""
		eff, err := time.ParseInLocation("2006-01-02", ch.Amender.EffFrom, now.Location())
		inForce := err == nil && !eff.After(now)
		if err == nil {
			when = ", từ " + eff.Format("02/01/2006")
		}
		var repl string
		switch {
		case inForce && to != "":
			repl = to + " [thay cho “" + from + "” theo " + label + when + "]"
		case inForce:
			repl = "[cụm từ “" + from + "” đã bị bãi bỏ theo " + label + when + "]"
		case to != "":
			repl = from + " [sẽ được thay bằng “" + to + "” theo " + label + when + "]"
		default:
			repl = from + " [sẽ bị bãi bỏ theo " + label + when + "]"
		}
		content = replaceInProvision(content, ch.Khoan, from, repl)
	}
	return content
}

// replaceInProvision replaces from with repl in khoản khoan of an article
// chunk ("" = everywhere in the chunk).
func replaceInProvision(content, khoan, from, repl string) string {
	if khoan == "" {
		return strings.ReplaceAll(content, from, repl)
	}
	locs := khoanStartRe.FindAllStringSubmatchIndex(content, -1)
	for i, l := range locs {
		if content[l[2]:l[3]] != khoan {
			continue
		}
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		return content[:l[0]] + strings.ReplaceAll(content[l[0]:end], from, repl) + content[end:]
	}
	return content
}
