-- Migration 000024 (#207) removed the last admin_settings row but kept the
-- table. Nothing reads or writes it any more (#220).
DROP TABLE IF EXISTS admin_settings;
