-- Add missing columns to transactions table
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS type VARCHAR(50);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS token_id BIGINT;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS contract_address VARCHAR(42);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS transaction_index INTEGER;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id);
