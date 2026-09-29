-- Migration: 000115_user_two_factor
-- TOTP two-factor authentication for user accounts (plan 2026-09-29).
--
--   - users.totp_secret: Base32 TOTP secret. Written by POST /auth/2fa/setup
--     (pending state) and only honoured once two_factor_enabled flips true.
--   - users.two_factor_enabled: master switch enforced by Login.
--   - users.two_factor_recovery_codes: JSON array of bcrypt hashes of the
--     one-time recovery codes minted at enable time. Plaintext codes are
--     shown to the user exactly once; a consumed code is removed from the
--     array. Empty string means no codes minted.
--
-- SQLite note: the repository's SQLite migration-test harness only replays
-- portable statements; ADD COLUMN IF NOT EXISTS is Postgres-only and is
-- covered on SQLite by the GORM model definition instead.

ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS two_factor_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS two_factor_recovery_codes TEXT NOT NULL DEFAULT '';
