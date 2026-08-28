-- Every account holder becomes a party.
--
-- Reads the Account Master directly. There is no preparation step: coercion,
-- trimming and typing happened when the dataset was ingested, and the dataset
-- IS that coerced table — the CSV was only how it arrived.
--
-- No `role` column: this rule only ever produces customers, so the payload says
-- so as a literal rather than carrying a column to vary something that never
-- varies. That column existed only when one payload shape served two rules.

SELECT
    -- FAS has no customer key. The holder exists only as a name on the account,
    -- so identity can only be per-account: two accounts for the same person are
    -- two parties until something better arrives.
    'FAS_CUST_' || "ACCT-NO"                        AS code,

    -- A joint holding is ONE party, not two. Qniform's model has a customer
    -- account name exactly one counterparty whatever its legal form.
    CASE WHEN "CUST-NAME-2" IS NOT NULL
         THEN "CUST-NAME-1" || ' & ' || "CUST-NAME-2"
         ELSE "CUST-NAME-1"
    END                                             AS name,

    ''                                              AS pre_name,
    __row
FROM account_master
WHERE "ACCT-TYPE-CD" <> 'H'     -- the firm's own book has no holder
  AND "CUST-NAME-1" IS NOT NULL
