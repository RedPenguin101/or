-- FAS partitions its account file by number range: the ledger an account sits in
-- *is* its class. The range table is the authority for the Qniform type, and
-- ACCT-TYPE-CD is not consulted here at all — its H means only "not a customer
-- account", which the range says more precisely.
--
-- The table is restated in each rule that needs it. There is nowhere shared to
-- put it: every SQL file belongs to exactly one rule, deliberately, so that no
-- rule logic can hide in an unclaimed model. See devnotes/design.md §3d.
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
    "ACCT-NO"                                       AS code,

    -- No ledger means no type. The account is then withheld at validation
    -- against Qniform's own spec, which is the right outcome: an account number
    -- in no known range is a discovery, not a row to guess a type for.
    ledger                                          AS "type",

    "ACCT-SHORT-NAME"                               AS name,

    CASE "COST-METHOD-CD"
        WHEN 'F' THEN 'fifo'
        WHEN 'L' THEN 'lifo'
        WHEN 'A' THEN 'wac'
        ELSE 'none'
    END                                             AS allocation_method,

    -- Required on custody and bank, optional on customer, and *rejected* on
    -- inventory and control — sending one there fails the whole request with a
    -- 400 rather than the item, so the ledger has to decide it here.
    CASE WHEN ledger IN ('customer', 'custody', 'bank')
         THEN "ACCT-NO"
    END                                             AS counterparty,

    -- Required on customer, optional on inventory, rejected on custody, control
    -- and bank: nobody is answerable for those in this sense.
    CASE WHEN ledger IN ('customer', 'inventory')
         THEN "REP-ID"
    END                                             AS responsible_party,
    __row
FROM classified
