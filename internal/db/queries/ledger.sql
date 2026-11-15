-- name: SchemaInstalled :one
SELECT
    coalesce(
        to_regclass('pgledger_accounts_view') IS NOT NULL
        AND to_regclass('pgledger_transfers_view') IS NOT NULL
        AND to_regclass('pgledger_entries_view') IS NOT NULL,
        FALSE
    )::BOOLEAN AS installed;

-- name: IdempotencyKeysInstalled :one
SELECT (to_regclass('pgledger_idempotency_keys_view') IS NOT NULL)::BOOLEAN AS installed;

-- name: ListAccounts :many
SELECT
    id,
    name,
    currency,
    balance::TEXT AS balance,
    version,
    allow_negative_balance,
    allow_positive_balance,
    metadata,
    created_at,
    updated_at
FROM pgledger_accounts_view
WHERE
    (
        id ILIKE sqlc.arg(prefix)::TEXT
        OR name ILIKE sqlc.arg(contains)::TEXT
        OR metadata::TEXT ILIKE sqlc.arg(contains)::TEXT
    )
    AND (sqlc.arg(meta_key)::TEXT = '' OR metadata ->> sqlc.arg(meta_key)::TEXT = sqlc.arg(meta_value)::TEXT)
ORDER BY (name ILIKE sqlc.arg(prefix)::TEXT) DESC, name ASC, id ASC
LIMIT sqlc.arg(row_limit)::BIGINT OFFSET sqlc.arg(row_offset)::BIGINT;

-- name: GetAccount :one
SELECT
    id,
    name,
    currency,
    balance::TEXT AS balance,
    version,
    allow_negative_balance,
    allow_positive_balance,
    metadata,
    created_at,
    updated_at
FROM pgledger_accounts_view
WHERE id = sqlc.arg(id)::TEXT;

-- name: ListTransfers :many
SELECT
    t.id,
    t.from_account_id,
    fa.name AS from_name,
    t.to_account_id,
    ta.name AS to_name,
    fa.currency,
    t.amount::TEXT AS amount,
    t.created_at,
    t.event_at,
    t.metadata,
    coalesce(k.idempotency_key, '')::TEXT AS idempotency_key
FROM pgledger_transfers_view t
INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
LEFT JOIN pgledger_idempotency_keys_view k ON t.id = k.transfer_id
WHERE
    (
        t.id ILIKE sqlc.arg(prefix)::TEXT
        OR k.idempotency_key ILIKE sqlc.arg(prefix)::TEXT
        OR t.metadata::TEXT ILIKE sqlc.arg(contains)::TEXT
        OR fa.name ILIKE sqlc.arg(contains)::TEXT
        OR ta.name ILIKE sqlc.arg(contains)::TEXT
    )
    AND (sqlc.arg(before)::TEXT = '' OR t.id < sqlc.arg(before)::TEXT)
ORDER BY t.id DESC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: GetTransfer :one
SELECT
    t.id,
    t.from_account_id,
    fa.name AS from_name,
    t.to_account_id,
    ta.name AS to_name,
    fa.currency,
    t.amount::TEXT AS amount,
    t.created_at,
    t.event_at,
    t.metadata,
    coalesce(k.idempotency_key, '')::TEXT AS idempotency_key
FROM pgledger_transfers_view t
INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
LEFT JOIN pgledger_idempotency_keys_view k ON t.id = k.transfer_id
WHERE t.id = sqlc.arg(id)::TEXT;

-- ListTransfersNoKeys and GetTransferNoKeys serve pgledger versions that
-- predate pgledger_idempotency_keys_view.

-- name: ListTransfersNoKeys :many
SELECT
    t.id,
    t.from_account_id,
    fa.name AS from_name,
    t.to_account_id,
    ta.name AS to_name,
    fa.currency,
    t.amount::TEXT AS amount,
    t.created_at,
    t.event_at,
    t.metadata,
    ''::TEXT AS idempotency_key
FROM pgledger_transfers_view t
INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
WHERE
    (
        t.id ILIKE sqlc.arg(prefix)::TEXT
        OR t.metadata::TEXT ILIKE sqlc.arg(contains)::TEXT
        OR fa.name ILIKE sqlc.arg(contains)::TEXT
        OR ta.name ILIKE sqlc.arg(contains)::TEXT
    )
    AND (sqlc.arg(before)::TEXT = '' OR t.id < sqlc.arg(before)::TEXT)
ORDER BY t.id DESC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: GetTransferNoKeys :one
SELECT
    t.id,
    t.from_account_id,
    fa.name AS from_name,
    t.to_account_id,
    ta.name AS to_name,
    fa.currency,
    t.amount::TEXT AS amount,
    t.created_at,
    t.event_at,
    t.metadata,
    ''::TEXT AS idempotency_key
FROM pgledger_transfers_view t
INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
WHERE t.id = sqlc.arg(id)::TEXT;

-- name: ListEntries :many
SELECT
    e.id,
    e.account_id,
    a.name AS account_name,
    a.currency,
    e.transfer_id,
    e.amount::TEXT AS amount,
    e.account_previous_balance::TEXT AS previous_balance,
    e.account_current_balance::TEXT AS current_balance,
    e.account_version,
    e.created_at,
    e.event_at,
    e.metadata,
    cp.id AS counterparty_id,
    cp.name AS counterparty_name
FROM pgledger_entries_view e
INNER JOIN pgledger_accounts_view a ON e.account_id = a.id
INNER JOIN pgledger_transfers_view t ON e.transfer_id = t.id
INNER JOIN pgledger_accounts_view cp
    ON cp.id = CASE WHEN t.from_account_id = e.account_id THEN t.to_account_id ELSE t.from_account_id END -- noqa: ST09
WHERE
    (
        e.id ILIKE sqlc.arg(prefix)::TEXT
        OR e.transfer_id ILIKE sqlc.arg(prefix)::TEXT
        OR a.name ILIKE sqlc.arg(contains)::TEXT
        OR e.metadata::TEXT ILIKE sqlc.arg(contains)::TEXT
    )
    AND (sqlc.arg(account_id)::TEXT = '' OR e.account_id = sqlc.arg(account_id)::TEXT)
    AND (sqlc.arg(before)::TEXT = '' OR e.id < sqlc.arg(before)::TEXT)
    AND (sqlc.narg(created_from)::TIMESTAMPTZ IS NULL OR e.created_at >= sqlc.narg(created_from)::TIMESTAMPTZ)
    AND (sqlc.narg(created_to)::TIMESTAMPTZ IS NULL OR e.created_at <= sqlc.narg(created_to)::TIMESTAMPTZ)
ORDER BY e.id DESC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: ListTransferEntries :many
SELECT
    e.id,
    e.account_id,
    a.name AS account_name,
    a.currency,
    e.transfer_id,
    e.amount::TEXT AS amount,
    e.account_previous_balance::TEXT AS previous_balance,
    e.account_current_balance::TEXT AS current_balance,
    e.account_version,
    e.created_at,
    e.event_at,
    e.metadata,
    cp.id AS counterparty_id,
    cp.name AS counterparty_name
FROM pgledger_entries_view e
INNER JOIN pgledger_accounts_view a ON e.account_id = a.id
INNER JOIN pgledger_transfers_view t ON e.transfer_id = t.id
INNER JOIN pgledger_accounts_view cp
    ON cp.id = CASE WHEN t.from_account_id = e.account_id THEN t.to_account_id ELSE t.from_account_id END -- noqa: ST09
WHERE e.transfer_id = sqlc.arg(transfer_id)::TEXT
ORDER BY e.amount ASC;

-- name: GetEntry :one
SELECT
    e.id,
    e.account_id,
    a.name AS account_name,
    a.currency,
    e.transfer_id,
    e.amount::TEXT AS amount,
    e.account_previous_balance::TEXT AS previous_balance,
    e.account_current_balance::TEXT AS current_balance,
    e.account_version,
    e.created_at,
    e.event_at,
    e.metadata,
    cp.id AS counterparty_id,
    cp.name AS counterparty_name
FROM pgledger_entries_view e
INNER JOIN pgledger_accounts_view a ON e.account_id = a.id
INNER JOIN pgledger_transfers_view t ON e.transfer_id = t.id
INNER JOIN pgledger_accounts_view cp
    ON cp.id = CASE WHEN t.from_account_id = e.account_id THEN t.to_account_id ELSE t.from_account_id END -- noqa: ST09
WHERE e.id = sqlc.arg(id)::TEXT;

-- name: MetadataPairs :many
SELECT
    kv.meta_key::TEXT AS meta_key,
    kv.meta_value::TEXT AS meta_value,
    count(*) AS transfers
FROM
    pgledger_transfers_view t,
    jsonb_each_text(CASE WHEN jsonb_typeof(t.metadata) = 'object' THEN t.metadata ELSE '{}'::JSONB END) kv (
        meta_key, meta_value
    )
WHERE kv.meta_key ILIKE sqlc.arg(pattern)::TEXT OR kv.meta_value ILIKE sqlc.arg(pattern)::TEXT
GROUP BY meta_key, meta_value
ORDER BY transfers DESC, meta_key ASC, meta_value ASC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: TraceKeys :many
SELECT
    k.meta_key::TEXT AS meta_key,
    count(*) AS transfers
FROM
    pgledger_transfers_view t,
    jsonb_object_keys(CASE WHEN jsonb_typeof(t.metadata) = 'object' THEN t.metadata ELSE '{}'::JSONB END) k (meta_key)
WHERE k.meta_key ILIKE sqlc.arg(pattern)::TEXT
GROUP BY meta_key
ORDER BY transfers DESC, meta_key ASC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: TraceValues :many
SELECT
    (metadata ->> sqlc.arg(meta_key)::TEXT)::TEXT AS meta_value,
    count(*) AS transfers,
    min(created_at)::TIMESTAMPTZ AS first_at,
    max(created_at)::TIMESTAMPTZ AS last_at
FROM pgledger_transfers_view
WHERE metadata ->> sqlc.arg(meta_key)::TEXT ILIKE sqlc.arg(pattern)::TEXT
GROUP BY meta_value
ORDER BY last_at DESC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: BalanceAtCreated :one
SELECT
    account_current_balance::TEXT AS amount,
    id AS entry_id,
    (count(*) OVER ())::BIGINT AS entries
FROM pgledger_entries_view
WHERE account_id = sqlc.arg(account_id)::TEXT AND created_at <= sqlc.arg(at)::TIMESTAMPTZ
ORDER BY account_version DESC
LIMIT 1;

-- name: BalanceAtEvent :one
SELECT
    coalesce(sum(amount), 0)::TEXT AS amount,
    count(*) AS entries
FROM pgledger_entries_view
WHERE account_id = sqlc.arg(account_id)::TEXT AND event_at <= sqlc.arg(at)::TIMESTAMPTZ;

-- name: BalanceHistory :many
SELECT
    id,
    created_at,
    transfer_id,
    account_current_balance::TEXT AS balance,
    account_previous_balance::TEXT AS previous_balance,
    account_version
FROM pgledger_entries_view
WHERE
    account_id = sqlc.arg(account_id)::TEXT
    AND (sqlc.narg(created_from)::TIMESTAMPTZ IS NULL OR created_at >= sqlc.narg(created_from)::TIMESTAMPTZ)
    AND (sqlc.narg(created_to)::TIMESTAMPTZ IS NULL OR created_at <= sqlc.narg(created_to)::TIMESTAMPTZ)
ORDER BY account_version DESC
LIMIT sqlc.arg(row_limit)::BIGINT;

-- name: AccountFlows :many
WITH flows AS (
    SELECT
        t.to_account_id = sqlc.arg(account_id)::TEXT AS incoming,
        CASE
            WHEN t.to_account_id = sqlc.arg(account_id)::TEXT THEN t.from_account_id ELSE t.to_account_id
        END AS counterparty_id,
        sum(t.amount) AS amount
    FROM pgledger_transfers_view t
    WHERE
        (t.from_account_id = sqlc.arg(account_id)::TEXT OR t.to_account_id = sqlc.arg(account_id)::TEXT)
        AND (sqlc.narg(created_from)::TIMESTAMPTZ IS NULL OR t.created_at >= sqlc.narg(created_from)::TIMESTAMPTZ)
        AND (sqlc.narg(created_to)::TIMESTAMPTZ IS NULL OR t.created_at <= sqlc.narg(created_to)::TIMESTAMPTZ)
    GROUP BY incoming, counterparty_id
),

ranked AS (
    SELECT
        incoming,
        counterparty_id,
        amount,
        sum(amount) OVER (PARTITION BY incoming) AS total,
        row_number() OVER (PARTITION BY incoming ORDER BY amount DESC, counterparty_id ASC) AS place
    FROM flows
)

SELECT
    r.incoming,
    r.counterparty_id::TEXT AS counterparty_id,
    a.name AS counterparty_name,
    r.amount::TEXT AS amount,
    r.total::TEXT AS total
FROM ranked r
INNER JOIN pgledger_accounts_view a ON r.counterparty_id = a.id
WHERE r.place <= sqlc.arg(top)::BIGINT
ORDER BY r.incoming DESC, r.place ASC;

-- name: GridEntries :many
WITH picked AS (
    SELECT
        t.id,
        t.created_at,
        t.event_at
    FROM pgledger_transfers_view t
    WHERE
        (
            sqlc.arg(account_id)::TEXT = ''
            OR t.from_account_id = sqlc.arg(account_id)::TEXT
            OR t.to_account_id = sqlc.arg(account_id)::TEXT
        )
        AND (sqlc.arg(meta_key)::TEXT = '' OR t.metadata ->> sqlc.arg(meta_key)::TEXT = sqlc.arg(meta_value)::TEXT)
        AND (sqlc.narg(created_from)::TIMESTAMPTZ IS NULL OR t.created_at >= sqlc.narg(created_from)::TIMESTAMPTZ)
        AND (sqlc.narg(created_to)::TIMESTAMPTZ IS NULL OR t.created_at <= sqlc.narg(created_to)::TIMESTAMPTZ)
    ORDER BY
        CASE WHEN sqlc.arg(by_event)::BOOLEAN THEN t.event_at ELSE t.created_at END DESC,
        t.created_at DESC,
        t.id DESC
    LIMIT sqlc.arg(max_transfers)::BIGINT
)

SELECT
    e.id AS entry_id,
    e.transfer_id,
    e.account_id,
    a.name AS account_name,
    a.currency,
    e.amount::TEXT AS amount,
    e.account_current_balance::TEXT AS balance_after,
    e.created_at,
    e.event_at,
    e.metadata
FROM picked p
INNER JOIN pgledger_entries_view e ON p.id = e.transfer_id
INNER JOIN pgledger_accounts_view a ON e.account_id = a.id
ORDER BY
    CASE WHEN sqlc.arg(by_event)::BOOLEAN THEN p.event_at ELSE p.created_at END ASC,
    p.created_at ASC,
    p.id ASC,
    e.amount ASC;
