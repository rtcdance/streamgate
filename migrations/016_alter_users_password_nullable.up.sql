-- Make password and email nullable for wallet-only auth users
-- Wallet-login users have no password and may not have an email
ALTER TABLE users ALTER COLUMN password DROP NOT NULL;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
