package tools

import (
	"container/list"
	"fmt"
	"strconv"
	"sync"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/types"
)

// The open-document prompt and the reading tools need the same .docx on
// every turn; downloading and parsing it again each time is the expensive
// part of a turn with several tabs. workspaceDocs keeps the bytes and the
// parsed layout of recently read documents, keyed by tenant, session and
// document, and trusts an entry only while the row still describes the
// same file (see workspaceDocVersion). Bounded by count and by bytes: a
// document may be up to 30 MB.
const (
	workspaceDocCacheEntries = 64
	workspaceDocCacheBytes   = 256 << 20
)

// workspaceDocVersion identifies the file a workspace row points at.
// Revision alone is not enough: an editor save (force-save before an AI
// edit, autosave) replaces CurrentRef and bumps SaveCount but keeps the
// revision, since the editor key must not change while it is open. A row
// without CurrentRef (no stored file to identify) is not cached.
func workspaceDocVersion(ws *types.DocumentWorkspace) string {
	if ws == nil || ws.CurrentRef == "" {
		return ""
	}
	return strconv.Itoa(ws.Revision) + "|" + strconv.Itoa(ws.SaveCount) + "|" + ws.CurrentRef
}

// workspaceDoc is one cached document. The bytes and the layout are shared
// by every reader and must not be modified.
type workspaceDoc struct {
	key     string
	version string
	content []byte
	layout  *docformat.Layout // parsed on first use
}

type workspaceDocCache struct {
	mu    sync.Mutex
	order *list.List // front = most recently used
	items map[string]*list.Element
	bytes int
}

var workspaceDocs = newWorkspaceDocCache()

func newWorkspaceDocCache() *workspaceDocCache {
	return &workspaceDocCache{order: list.New(), items: map[string]*list.Element{}}
}

func workspaceDocKey(tenantID uint64, sessionID, documentID string) string {
	return fmt.Sprintf("%d|%s|%s", tenantID, sessionID, documentID)
}

// get returns the cached document when its version still matches, or nil.
func (c *workspaceDocCache) get(key, version string) *workspaceDoc {
	if version == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil
	}
	doc := el.Value.(*workspaceDoc)
	if doc.version != version {
		return nil
	}
	c.order.MoveToFront(el)
	return doc
}

// put stores the bytes read for ws, replacing an older version.
func (c *workspaceDocCache) put(key string, ws *types.DocumentWorkspace, content []byte) *workspaceDoc {
	doc := &workspaceDoc{key: key, version: workspaceDocVersion(ws), content: content}
	c.mu.Lock()
	defer c.mu.Unlock()
	if doc.version == "" || len(content) > workspaceDocCacheBytes {
		return doc
	}
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	c.items[key] = c.order.PushFront(doc)
	c.bytes += len(content)
	for c.order.Len() > workspaceDocCacheEntries || c.bytes > workspaceDocCacheBytes {
		c.remove(c.order.Back())
	}
	return doc
}

func (c *workspaceDocCache) remove(el *list.Element) {
	doc := el.Value.(*workspaceDoc)
	c.order.Remove(el)
	delete(c.items, doc.key)
	c.bytes -= len(doc.content)
}

// layoutOf parses doc once; later callers share the result.
func (c *workspaceDocCache) layoutOf(doc *workspaceDoc) *docformat.Layout {
	c.mu.Lock()
	l := doc.layout
	c.mu.Unlock()
	if l != nil {
		return l
	}
	l = docformat.InspectDocx(doc.content)
	c.mu.Lock()
	if doc.layout == nil {
		doc.layout = l
	}
	l = doc.layout
	c.mu.Unlock()
	return l
}
