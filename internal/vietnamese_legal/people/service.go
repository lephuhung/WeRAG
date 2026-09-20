package people

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SearchResult is the outcome of one people lookup — mirrors the dict the
// AIRAG generators yielded: found/persons/display/schemas/lookup_type, plus
// Unavailable for the "system busy" fallback (distinct from a real empty hit).
type SearchResult struct {
	Found       bool           `json:"found"`
	Persons     []PersonRecord `json:"persons"`
	Display     string         `json:"display"`
	Schemas     []string       `json:"schemas,omitempty"`
	LookupType  string         `json:"lookup_type"`
	Unavailable bool           `json:"unavailable,omitempty"`
}

func busyResult(lookupType string) *SearchResult {
	return &SearchResult{Found: false, Display: busyMessage, LookupType: lookupType, Unavailable: true}
}

func notFound(lookupType, display string) *SearchResult {
	return &SearchResult{Found: false, Display: display, LookupType: lookupType}
}

// normalizePhone strips separators — mirrors _normalize_phone.
var phoneSep = regexp.MustCompile(`[\s\-.]+`)

func normalizePhone(p string) string { return phoneSep.ReplaceAllString(p, "") }

// extractNumbers ports _extract_numbers: digit groups may carry internal
// separators ("0912 345 678"), which are merged first; validLengths filters
// by group length, minLen applies when validLengths is empty.
func extractNumbers(text string, validLengths []int, minLen int) []string {
	// Merge digit groups separated only by [\s.\-]: drop a separator run when
	// a digit sits on both sides.
	var merged strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == ' ' || r == '.' || r == '-' {
			j := i - 1
			for j >= 0 && (runes[j] == ' ' || runes[j] == '.' || runes[j] == '-') {
				j--
			}
			k := i + 1
			for k < len(runes) && (runes[k] == ' ' || runes[k] == '.' || runes[k] == '-') {
				k++
			}
			if j >= 0 && k < len(runes) &&
				runes[j] >= '0' && runes[j] <= '9' && runes[k] >= '0' && runes[k] <= '9' {
				continue
			}
		}
		merged.WriteRune(r)
	}
	groups := regexp.MustCompile(`\d+`).FindAllString(merged.String(), -1)
	var out []string
	seen := map[string]bool{}
	for _, g := range groups {
		if len(validLengths) > 0 {
			ok := false
			for _, l := range validLengths {
				if len(g) == l {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		} else if len(g) < minLen {
			continue
		}
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}

func limitFor(nValues int, base int64) int64 {
	if nValues <= 1 {
		return base
	}
	v := int64(nValues) * base
	if v > 100 {
		v = 100
	}
	return v
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// queryOneSchema runs one collection query, mirroring _query_single_schema_sync:
// "exact" ($in with an int variant for all-digit values), "regex" (substring,
// case-insensitive), "phone" (normalized $in). Per-schema timeout or query
// errors return (nil, nil) — the collection is skipped; connection failures
// return errUnavailable.
func (s *Service) queryOneSchema(
	ctx context.Context,
	db *mongo.Database,
	schema string,
	cfg SchemaConfig,
	matchMode string,
	values []string,
	limit int64,
) ([]bson.D, error) {
	var filter bson.M
	switch matchMode {
	case "exact":
		// Exact match on INDEXED fields (CCCD/CMND/BHXH digits). Include the
		// int variant so documents stored as numbers still match — without a
		// regex 'i' option so the index stays usable.
		inVals := make(bson.A, 0, len(values)*2)
		for _, v := range values {
			inVals = append(inVals, v)
			if isAllDigits(v) {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					inVals = append(inVals, n)
				}
			}
		}
		or := make(bson.A, 0, len(cfg.Fields))
		for _, f := range cfg.Fields {
			or = append(or, bson.M{f: bson.M{"$in": inVals}})
		}
		filter = bson.M{"$or": or}
	case "regex":
		or := bson.A{}
		for _, f := range cfg.Fields {
			for _, v := range values {
				or = append(or, bson.M{f: bson.Regex{Pattern: regexp.QuoteMeta(v), Options: "i"}})
			}
		}
		filter = bson.M{"$or": or}
	case "phone":
		norm := make(bson.A, 0, len(values))
		for _, v := range values {
			norm = append(norm, normalizePhone(v))
		}
		or := make(bson.A, 0, len(cfg.Fields))
		for _, f := range cfg.Fields {
			or = append(or, bson.M{f: bson.M{"$in": norm}})
		}
		filter = bson.M{"$or": or}
	default:
		return nil, nil
	}

	qctx, cancel := context.WithTimeout(ctx, s.cfg.QueryTimeout)
	defer cancel()
	cursor, err := db.Collection(schema).Find(qctx, filter, options.Find().SetLimit(limit))
	if err != nil {
		return nil, s.classifyErr(schema, err)
	}
	var docs []bson.D
	if err := cursor.All(qctx, &docs); err != nil {
		return nil, s.classifyErr(schema, err)
	}
	return docs, nil
}

// classifyErr maps a query error to nil (skip schema) or errUnavailable
// (connection-level failure).
func (s *Service) classifyErr(schema string, err error) error {
	if isConnError(err) {
		logger.GetLogger(context.Background()).Errorf("[people] connection error on %s: %v", schema, err)
		return errUnavailable
	}
	if isTimeout(err) {
		logger.GetLogger(context.Background()).Warnf(
			"[people] TIMEOUT %s (> %s — collection may lack an index) → skipped", schema, s.cfg.QueryTimeout)
		return nil
	}
	logger.GetLogger(context.Background()).Warnf("[people] query error on %s: %v", schema, err)
	return nil
}

// gatherSchemas fans the per-schema queries out in parallel and merges the
// hits. A total connection failure with zero data → errUnavailable; partial
// failures still return whatever collections answered.
func (s *Service) gatherSchemas(
	ctx context.Context,
	db *mongo.Database,
	lookupType, matchMode string,
	values []string,
	limit int64,
) (map[string][]bson.D, error) {
	schemaMap := SearchableCollectionMap[lookupType].Collections

	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[string][]bson.D{}
	unavailable := false

	for schema, cfg := range schemaMap {
		fields := cfg.Fields
		if len(fields) == 0 && len(cfg.CanonFields) > 0 {
			for _, f := range cfg.CanonFields {
				fields = append(fields, f)
			}
		}
		if len(fields) == 0 {
			continue
		}
		wg.Add(1)
		go func(schema string, cfg SchemaConfig) {
			defer wg.Done()
			docs, err := s.queryOneSchema(ctx, db, schema, cfg, matchMode, values, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				unavailable = true
				return
			}
			if len(docs) > 0 {
				results[schema] = append(results[schema], docs...)
			}
		}(schema, cfg)
	}
	wg.Wait()

	if unavailable && len(results) == 0 {
		return nil, errUnavailable
	}
	return results, nil
}

// searchMulti is the shared multi-schema path for cccd/bhxh/phone/name —
// mirrors _search_multi (minus the generator: we return the final result).
func (s *Service) searchMulti(
	ctx context.Context, lookupType string, values []string, matchMode string, perSchemaLimit int64,
) *SearchResult {
	if _, ok := SearchableCollectionMap[lookupType]; !ok {
		return notFound(lookupType, "Không hỗ trợ lookup type: "+lookupType)
	}
	db, err := s.database(ctx)
	if err != nil {
		return busyResult(lookupType)
	}
	results, err := s.gatherSchemas(ctx, db, lookupType, matchMode, values, perSchemaLimit)
	if err != nil {
		return busyResult(lookupType)
	}
	persons, display, schemas := consolidate(results, lookupType)
	if len(persons) == 0 {
		return nil // caller renders its own not-found message
	}
	return &SearchResult{
		Found: true, Persons: persons, Display: display, Schemas: schemas, LookupType: lookupType,
	}
}

// queryOneSchemaAdvanced runs the AND-of-criteria query on one collection —
// mirrors _query_single_schema_advanced_sync: name matches exactly
// (^...$ i), dob/address match as substrings, phone matches the normalized
// value exactly.
func (s *Service) queryOneSchemaAdvanced(
	ctx context.Context,
	db *mongo.Database,
	schema string,
	canonFields map[string]string,
	criteria map[string]string,
	limit int64,
) ([]bson.D, error) {
	var and bson.A
	for key, value := range criteria {
		if value == "" {
			continue
		}
		field, ok := canonFields[key]
		if !ok || field == "" {
			continue
		}
		switch key {
		case "phone":
			and = append(and, bson.M{field: normalizePhone(value)})
		case "name":
			and = append(and, bson.M{field: bson.Regex{Pattern: "^" + regexp.QuoteMeta(value) + "$", Options: "i"}})
		default:
			and = append(and, bson.M{field: bson.Regex{Pattern: regexp.QuoteMeta(value), Options: "i"}})
		}
	}
	if len(and) == 0 {
		return nil, nil
	}
	qctx, cancel := context.WithTimeout(ctx, s.cfg.QueryTimeout)
	defer cancel()
	cursor, err := db.Collection(schema).Find(qctx, bson.M{"$and": and}, options.Find().SetLimit(limit))
	if err != nil {
		return nil, s.classifyErr(schema, err)
	}
	var docs []bson.D
	if err := cursor.All(qctx, &docs); err != nil {
		return nil, s.classifyErr(schema, err)
	}
	return docs, nil
}

// ============================================================================
// Public API — one method per lookup type (AIRAG search_by_* equivalents)
// ============================================================================

// SearchByCCCD finds persons by one or more CCCD/CMND numbers (9 or 12
// digits) extracted from the raw input.
func (s *Service) SearchByCCCD(ctx context.Context, raw string) *SearchResult {
	values := extractNumbers(raw, []int{9, 12}, 0)
	if len(values) == 0 {
		return notFound("cccd", "Không tìm thấy số CCCD hợp lệ (cần 9 hoặc 12 chữ số).")
	}
	res := s.searchMulti(ctx, "cccd", values, "exact", limitFor(len(values), s.cfg.PerSchemaLimit))
	if res == nil {
		return notFound("cccd", "Không tìm thấy người có CCCD: "+strings.Join(values, ", "))
	}
	return res
}

// SearchByBHXH finds persons by one or more BHXH numbers (>=5 digits).
func (s *Service) SearchByBHXH(ctx context.Context, raw string) *SearchResult {
	values := extractNumbers(raw, nil, 5)
	if len(values) == 0 {
		return notFound("bhxh", "Không tìm thấy số BHXH hợp lệ (cần ít nhất 5 chữ số).")
	}
	res := s.searchMulti(ctx, "bhxh", values, "exact", limitFor(len(values), s.cfg.PerSchemaLimit))
	if res == nil {
		return notFound("bhxh", "Không tìm thấy người có số BHXH: "+strings.Join(values, ", "))
	}
	return res
}

// SearchByPhone finds persons by one or more phone numbers (exactly 10 digits).
func (s *Service) SearchByPhone(ctx context.Context, raw string) *SearchResult {
	values := extractNumbers(raw, []int{10}, 0)
	if len(values) == 0 {
		return notFound("phone", "Không tìm thấy số điện thoại hợp lệ (cần đúng 10 chữ số).")
	}
	res := s.searchMulti(ctx, "phone", values, "phone", limitFor(len(values), s.cfg.PerSchemaLimit))
	if res == nil {
		return notFound("phone", "Không tìm thấy người có số điện thoại: "+strings.Join(values, ", "))
	}
	return res
}

// SearchByName finds persons by name (substring regex, case-insensitive).
func (s *Service) SearchByName(ctx context.Context, name string, limit int64) *SearchResult {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 2 {
		return notFound("name", "Tên tìm kiếm quá ngắn.")
	}
	if limit <= 0 {
		limit = s.cfg.PerSchemaLimit
	}
	res := s.searchMulti(ctx, "name", []string{name}, "regex", limit)
	if res == nil {
		return notFound("name", "Không tìm thấy người có tên: "+name)
	}
	return res
}

// SearchAdvanced finds persons by an AND combination of criteria
// (name/dob/address/phone). Collections that do not map any of the supplied
// criteria are skipped — mirrors _search_multi_advanced.
func (s *Service) SearchAdvanced(ctx context.Context, criteria map[string]string, limit int64) *SearchResult {
	const lookupType = "advanced"
	clean := map[string]string{}
	for k, v := range criteria {
		if t := strings.TrimSpace(v); t != "" {
			clean[k] = t
		}
	}
	if len(clean) == 0 {
		return notFound(lookupType, "Không có tiêu chí tìm kiếm hợp lệ.")
	}
	if limit <= 0 {
		limit = s.cfg.PerSchemaLimit
	}

	db, err := s.database(ctx)
	if err != nil {
		return busyResult(lookupType)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[string][]bson.D{}
	unavailable := false
	anyTask := false

	for schema, cfg := range SearchableCollectionMap[lookupType].Collections {
		// Only query collections that map at least one supplied criterion.
		supported := false
		for k := range clean {
			if _, ok := cfg.CanonFields[k]; ok {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		anyTask = true
		wg.Add(1)
		go func(schema string, canon map[string]string) {
			defer wg.Done()
			docs, err := s.queryOneSchemaAdvanced(ctx, db, schema, canon, clean, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				unavailable = true
				return
			}
			if len(docs) > 0 {
				results[schema] = append(results[schema], docs...)
			}
		}(schema, cfg.CanonFields)
	}
	wg.Wait()

	if !anyTask {
		return notFound(lookupType, "Không có collection nào hỗ trợ các trường tìm kiếm này.")
	}
	if unavailable && len(results) == 0 {
		return busyResult(lookupType)
	}
	persons, display, schemas := consolidate(results, lookupType)
	if len(persons) == 0 {
		return notFound(lookupType, fmt.Sprintf("Không tìm thấy người khớp với thông tin: %v", clean))
	}
	return &SearchResult{
		Found: true, Persons: persons, Display: display, Schemas: schemas, LookupType: lookupType,
	}
}
