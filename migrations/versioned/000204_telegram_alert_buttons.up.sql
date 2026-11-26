-- Migration: 000204_telegram_alert_buttons
-- Order buttons on the Telegram alert bot.
--
-- telegram_alert_messages.buttons: the inline keyboard of an alert, rows of
-- {text, callback_data | url}.
--
-- telegram_alert_chats.can_act: the chat is the linking SuperAdmin's OWN
-- Telegram account (linked with a personal code, private chat only), so a
-- press from that Telegram user may confirm or cancel orders as that
-- SuperAdmin. Chats linked before, or with an ordinary code (a group, a
-- colleague who only receives alerts), stay FALSE.
DO $$ BEGIN RAISE NOTICE '[Migration 000204] Adding telegram_alert_messages.buttons, telegram_alert_chats.can_act'; END $$;

ALTER TABLE telegram_alert_messages ADD COLUMN IF NOT EXISTS buttons JSONB NULL;
ALTER TABLE telegram_alert_chats ADD COLUMN IF NOT EXISTS can_act BOOLEAN NOT NULL DEFAULT FALSE;
