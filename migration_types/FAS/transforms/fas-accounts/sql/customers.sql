SELECT
    'FAS_CUST_' || "ACCT-NO"                        AS code,

    CASE WHEN "CUST-NAME-2" IS NOT NULL
         THEN "CUST-NAME-1" || ' & ' || "CUST-NAME-2"
         ELSE "CUST-NAME-1"
    END                                             AS name,

    ''                                              AS pre_name,
    __row
FROM account_master
WHERE "ACCT-TYPE-CD" <> 'H'
  AND "CUST-NAME-1" IS NOT NULL
