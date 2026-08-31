SELECT
    "ACCT-NO"                                       AS code,

    CASE WHEN "ACCT-TYPE-CD" = 'H'
         THEN 'inventory' ELSE 'customer'
    END                                             AS "type",

    "ACCT-SHORT-NAME"                               AS name,

    CASE "COST-METHOD-CD"
        WHEN 'F' THEN 'fifo'
        WHEN 'L' THEN 'lifo'
        WHEN 'A' THEN 'wac'
        ELSE 'none'
    END                                             AS allocation_method,

    CASE WHEN "ACCT-TYPE-CD" <> 'H'
         THEN "ACCT-NO"
    END                                             AS counterparty,

    "REP-ID"                                        AS responsible_party,
    __row
FROM account_master
