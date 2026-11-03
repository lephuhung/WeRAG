-- Migration: 000185_usage_order_invoice
-- A buyer may ask for a VAT invoice (hóa đơn GTGT) on an order: the
-- invoice details (company name, tax code, address, email that receives
-- the e-invoice) stay editable until a SuperAdmin marks the invoice
-- issued. The e-invoice itself is issued in an external system; the order
-- only keeps its number, series and date.
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_requested BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_company_name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_tax_code VARCHAR(14) NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_address TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_email VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_issued_at TIMESTAMPTZ;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_issued_by VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_no VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_series VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS invoice_date TIMESTAMPTZ;
-- the SuperAdmin's to-do list: paid orders asking for an invoice not yet issued
CREATE INDEX IF NOT EXISTS idx_usage_orders_invoice_to_issue
    ON usage_orders (paid_at)
    WHERE invoice_requested AND invoice_issued_at IS NULL AND status = 'paid';
