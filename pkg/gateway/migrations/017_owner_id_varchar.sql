-- Change owner_id from UUID to VARCHAR to support wallet addresses
ALTER TABLE uploads ALTER COLUMN owner_id TYPE VARCHAR(128);
ALTER TABLE contents ALTER COLUMN owner_id TYPE VARCHAR(128);
