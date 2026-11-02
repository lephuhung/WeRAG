DROP TABLE IF EXISTS usage_subscription_addons;
ALTER TABLE custom_agents DROP CONSTRAINT IF EXISTS chk_custom_agents_distribution;
ALTER TABLE custom_agents DROP COLUMN IF EXISTS distribution;
