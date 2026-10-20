DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_sessions_user_id;
DROP INDEX IF EXISTS idx_sessions_previous_token_hash;
DROP INDEX IF EXISTS idx_sessions_token_hash;
DROP TABLE IF EXISTS sessions;