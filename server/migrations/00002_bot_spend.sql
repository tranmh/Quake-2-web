-- +goose Up
-- Daily USD spend of the server's AI bots on paid decision backends (Jev),
-- shared by every q2server process using this database
-- (agent/budget.Account via db.BotSpendStore).
CREATE TABLE bot_spend (
    day        date             PRIMARY KEY,
    usd        double precision NOT NULL DEFAULT 0 CHECK (usd >= 0),
    updated_at timestamptz      NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE bot_spend;
