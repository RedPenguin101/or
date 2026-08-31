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
    'FAS_CPTY_' || "ACCT-NO"                        AS code,

    -- A joint holding is one party. ACCT-TYPE-CD = J is what licenses reading
    -- CUST-NAME-2 as a name: on other registration forms the column is as often
    -- an address line.
    CASE WHEN "ACCT-TYPE-CD" = 'J' AND "CUST-NAME-2" IS NOT NULL
         THEN "CUST-NAME-1" || ' & ' || "CUST-NAME-2"
         ELSE "CUST-NAME-1"
    END                                             AS name,

    ''                                              AS pre_name,

    -- One column, three roles, decided by the ledger. Qniform keeps custodian
    -- and bank apart from customer because a safekeeping obligation, a deposit
    -- liability and a client relationship are different things, and each account
    -- type will only accept its own.
    CASE ledger
        WHEN 'customer' THEN 'customer'
        WHEN 'custody'  THEN 'custodian'
        WHEN 'bank'     THEN 'bank'
    END                                             AS role,
    __row
FROM classified
WHERE ledger IN ('customer', 'custody', 'bank')
  AND "CUST-NAME-1" IS NOT NULL
