-- One account per source record.
--
-- Columns are named exactly as Qniform names the fields they become. The model
-- produces the target's own shape, flattened; it is not a third vocabulary
-- sitting between the two.
--
-- The two party columns hold the *raw* source keys. Their prefixes are applied
-- by the rule's declared references, so this model never restates a mint and the
-- two cannot drift apart.

SELECT
    "ACCT-NO"                                       AS code,

    -- ACCT-TYPE-CD is the pivot: it decides the account type, whether a
    -- counterparty is permitted at all, and which role the responsible party
    -- must hold.
    CASE WHEN "ACCT-TYPE-CD" = 'H'
         THEN 'inventory' ELSE 'customer'
    END                                             AS "type",

    "ACCT-SHORT-NAME"                               AS name,

    CASE "COST-METHOD-CD"
        WHEN 'F' THEN 'fifo'
        WHEN 'L' THEN 'lifo'
        WHEN 'A' THEN 'wac'
        ELSE 'none'                                 -- blank means none, not absent
    END                                             AS allocation_method,

    -- NULL on a house account: sending a counterparty there fails the whole
    -- request with a 400, and a NULL column is an omitted key.
    CASE WHEN "ACCT-TYPE-CD" <> 'H'
         THEN "ACCT-NO"
    END                                             AS counterparty,

    "REP-ID"                                        AS responsible_party,
    __row
FROM account_master
