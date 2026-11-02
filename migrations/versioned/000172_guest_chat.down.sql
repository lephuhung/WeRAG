-- Rollback for 000172_guest_chat: the seeded guest plan (by its fixed id,
-- never another plan coded 'guest') with its subscriptions, and the public
-- portal with the Guests' chats held in it.
--
-- An order references the plan (usage_orders.plan_id, no cascade) only if
-- someone sold it after the seed: then the plan and its subscriptions stay,
-- as paid history must, and a notice says so.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM usage_orders WHERE plan_id = '00000000-0000-4000-8000-000000000172') THEN
        RAISE NOTICE '[Migration 000172 down] guest plan kept: orders reference it';
    ELSE
        DELETE FROM usage_subscriptions WHERE plan_id = '00000000-0000-4000-8000-000000000172';
        DELETE FROM usage_plans WHERE id = '00000000-0000-4000-8000-000000000172';
    END IF;
END $$;

DELETE FROM messages
WHERE session_id IN (
    SELECT s.id FROM sessions s
    JOIN tenants t ON t.id = s.tenant_id
    WHERE t.kind = 'public_portal'
);
DELETE FROM sessions
WHERE tenant_id IN (SELECT id FROM tenants WHERE kind = 'public_portal');
DELETE FROM tenants WHERE kind = 'public_portal';
