-- One account per source record.
--
-- Columns are named exactly as Qniform names the fields they become. The model
-- produces the target's own shape, flattened; it is not a third vocabulary
-- sitting between the two.
--
-- The party codes named here are minted by the same expressions the party rules
-- use. If the two ever drift, every account referencing a party would be
-- withheld for an unresolvable reference — which the cascade test asserts
-- against, so the duplication cannot rot silently.

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

    CASE WHEN "REP-ID" IS NOT NULL
         THEN 'FAS_REP_' || "REP-ID"
    END                                             AS responsible_party,

    CASE WHEN "ACCT-TYPE-CD" <> 'H'
         THEN 'FAS_CUST_' || "ACCT-NO"
    END                                             AS counterparty,

    __row
FROM account_master
