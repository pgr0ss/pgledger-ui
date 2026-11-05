-- name: SampleDataPresent :one
SELECT exists(
    SELECT 1 FROM pgledger_accounts_view
    WHERE name = 'liquidity.usd'
) AS present;

-- name: CreateAccount :one
SELECT id
FROM
    pgledger_create_account(
        sqlc.arg(name)::TEXT,
        sqlc.arg(currency)::TEXT,
        sqlc.arg(allow_negative_balance)::BOOLEAN,
        sqlc.arg(allow_positive_balance)::BOOLEAN,
        sqlc.narg(metadata)::JSONB
    );

-- name: CreateTransfer :one
SELECT id
FROM
    pgledger_create_transfer(
        sqlc.arg(from_account_id)::TEXT,
        sqlc.arg(to_account_id)::TEXT,
        cast(sqlc.arg(amount)::TEXT AS NUMERIC),
        event_at => sqlc.arg(event_at)::TIMESTAMPTZ,
        metadata => sqlc.narg(metadata)::JSONB
    );

-- name: CreateFxTransfers :many
SELECT id
FROM
    pgledger_create_transfers(
        transfer_requests => ARRAY[
            (
                sqlc.arg(from1)::TEXT,
                sqlc.arg(to1)::TEXT,
                cast(sqlc.arg(amount1)::TEXT AS NUMERIC)
            ),
            (
                sqlc.arg(from2)::TEXT,
                sqlc.arg(to2)::TEXT,
                cast(sqlc.arg(amount2)::TEXT AS NUMERIC)
            )
        ]::TRANSFER_REQUEST[],
        event_at => sqlc.arg(event_at)::TIMESTAMPTZ,
        metadata => sqlc.narg(metadata)::JSONB
    );

-- pgledger stamps rows with now(), so sample data overwrites created_at to spread it over time.
-- name: BackdateTransfers :exec
WITH transfers AS (
    UPDATE pgledger_transfers SET created_at = sqlc.arg(created_at)::TIMESTAMPTZ
    WHERE id = any(sqlc.arg(ids)::TEXT[])
)

UPDATE pgledger_entries SET created_at = sqlc.arg(created_at)::TIMESTAMPTZ
WHERE transfer_id = any(sqlc.arg(ids)::TEXT[]);

-- name: BackdateAccounts :exec
UPDATE pgledger_accounts a
SET
    created_at = sqlc.arg(created_at)::TIMESTAMPTZ,
    updated_at = coalesce(
        (
            SELECT max(e.created_at) FROM pgledger_entries e
            WHERE e.account_id = a.id
        ),
        sqlc.arg(created_at)::TIMESTAMPTZ
    )
WHERE a.id = any(sqlc.arg(ids)::TEXT[]);
