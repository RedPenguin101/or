-- One party per distinct rep or trading desk.
--
-- The same REP-ID appears on many accounts with inconsistent REP-NAME
-- spellings. The identity is the ID; the name disagreement is surfaced by the
-- conflict rule in transform.yaml rather than silently resolved here, because
-- picking a spelling is a decision someone has to own and record.

SELECT
    'FAS_REP_' || "REP-ID"                          AS code,
    "REP-NAME"                                      AS name,
    ''                                              AS pre_name,

    -- One column, two meanings: on a customer account REP-ID is a registered
    -- rep, on a house account it is a trading desk. Qniform will not accept one
    -- where it wants the other, so the role is decided by ACCT-TYPE-CD. This one
    -- genuinely varies by row, so unlike the customers rule it needs a column.
    CASE WHEN "ACCT-TYPE-CD" = 'H'
         THEN 'trader' ELSE 'rep'
    END                                             AS role,
    __row
FROM account_master
WHERE "REP-ID" IS NOT NULL      -- 0000 means unassigned, and coerces to absent
