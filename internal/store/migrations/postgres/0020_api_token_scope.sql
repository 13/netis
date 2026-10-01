-- What an API token may do: 'full' acts with its owner's role, 'read' only
-- reads (GET and HEAD). Tokens issued before scopes existed stay full.
ALTER TABLE api_token ADD COLUMN scope TEXT NOT NULL DEFAULT 'full';
