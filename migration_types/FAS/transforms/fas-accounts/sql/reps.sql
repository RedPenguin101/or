-- The same range table as accounts.sql, for the same reason (design.md §3d).
WITH numbered AS (
    SELECT *, TRY_CAST("ACCT-NO" AS BIGINT) AS acct_num
    FROM account_master
),
classified AS (
    SELECT *,
        CASE
            WHEN acct_num BETWEEN    10000 AND    99999 THEN 'bank'
            WHEN acct_num BETWEEN   100000 AND 89999999 THEN 'customer'
            WHEN acct_num BETWEEN 91000000 AND 92999999 THEN 'inventory'
            WHEN acct_num BETWEEN 93000000 AND 93899999 THEN 'control'
            WHEN acct_num BETWEEN 93900000 AND 93999999 THEN 'custody'
            WHEN acct_num BETWEEN 95000000 AND 95999999 THEN 'control'
            WHEN acct_num BETWEEN 99999997 AND 99999999 THEN 'control'
        END AS ledger
    FROM numbered
)
SELECT
    'FAS_REP_' || "REP-ID"                          AS code,
    "REP-NAME"                                      AS name,
    ''                                              AS pre_name,

    CASE WHEN ledger = 'inventory'
         THEN 'trader' ELSE 'rep'
    END                                             AS role,
    __row
FROM classified
-- Only the two ledgers whose accounts may name a responsible party. A REP-ID
-- left on a bank or control record is stale denormalisation, not a party.
WHERE ledger IN ('customer', 'inventory')
  AND "REP-ID" IS NOT NULL
