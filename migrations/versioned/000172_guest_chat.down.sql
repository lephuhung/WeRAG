-- Rollback for 000172_guest_chat: the guest plan with its subscriptions,
-- and the public portal with the Guests' chats held in it.
DELETE FROM usage_subscriptions
WHERE plan_id IN (SELECT id FROM usage_plans WHERE code = 'guest');
DELETE FROM usage_plans WHERE code = 'guest';

DELETE FROM messages
WHERE session_id IN (
    SELECT s.id FROM sessions s
    JOIN tenants t ON t.id = s.tenant_id
    WHERE t.kind = 'public_portal'
);
DELETE FROM sessions
WHERE tenant_id IN (SELECT id FROM tenants WHERE kind = 'public_portal');
DELETE FROM tenants WHERE kind = 'public_portal';
