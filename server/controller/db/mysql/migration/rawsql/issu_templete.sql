-- This templete is for upgrade using CREATE/DROP/ALTER
-- modify start, add upgrade sql
-- example
ALTER TABLE go_genesis_ip ADD node_ip CHAR(48) DEFAULT NULL;
-- update db_version to latest, remeber update DB_VERSION_EXPECTED in migration/version.go
UPDATE db_version SET version='6.1.1.0';
-- modify end
