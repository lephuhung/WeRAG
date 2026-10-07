package types

import "time"

// KBSubscription puts a published knowledge base (KBVisibilityPublished)
// into a reader's default retrieval scope. UserID == "" subscribes the
// whole tenant (set by its Tenant Admin); otherwise it subscribes one
// member of TenantID. Reading a published KB never needs a subscription —
// it only decides whether chat searches it without an explicit @mention.
type KBSubscription struct {
	ID        string    `json:"id"         gorm:"type:varchar(36);primaryKey"`
	KBID      string    `json:"kb_id"      gorm:"column:kb_id;type:varchar(36);not null;uniqueIndex:uniq_kb_subscriptions_subject"`
	TenantID  uint64    `json:"tenant_id"  gorm:"not null;uniqueIndex:uniq_kb_subscriptions_subject"`
	UserID    string    `json:"user_id"    gorm:"type:varchar(36);not null;default:'';uniqueIndex:uniq_kb_subscriptions_subject"`
	CreatedBy string    `json:"created_by" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName binds KBSubscription to the kb_subscriptions table.
func (KBSubscription) TableName() string { return "kb_subscriptions" }

// Public catalog kinds for GET /knowledge-bases/public?kind=.
const (
	// PublicCatalogKindPlatform lists platform-owned public KBs (default).
	PublicCatalogKindPlatform = "platform"
	// PublicCatalogKindPublished lists tenant-published KBs.
	PublicCatalogKindPublished = "published"
)
