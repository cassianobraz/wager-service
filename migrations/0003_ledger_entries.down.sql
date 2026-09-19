DROP TRIGGER IF EXISTS ledger_entries_no_delete ON ledger_entries;
DROP TRIGGER IF EXISTS ledger_entries_no_update ON ledger_entries;
DROP FUNCTION IF EXISTS ledger_entries_immutable();
DROP TABLE IF EXISTS ledger_entries;
