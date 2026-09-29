-- Rollback for 000115_user_two_factor.
--
-- Disables 2FA for every account and discards pending TOTP secrets and
-- recovery codes. Users who had 2FA on must re-enrol after re-applying.

ALTER TABLE users DROP COLUMN IF EXISTS two_factor_recovery_codes;
ALTER TABLE users DROP COLUMN IF EXISTS two_factor_enabled;
ALTER TABLE users DROP COLUMN IF EXISTS totp_secret;
