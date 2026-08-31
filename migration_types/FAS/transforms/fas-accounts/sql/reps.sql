SELECT
    'FAS_REP_' || "REP-ID"                          AS code,
    "REP-NAME"                                      AS name,
    ''                                              AS pre_name,

    CASE WHEN "ACCT-TYPE-CD" = 'H'
         THEN 'trader' ELSE 'rep'
    END                                             AS role,
    __row
FROM account_master
WHERE "REP-ID" IS NOT NULL
