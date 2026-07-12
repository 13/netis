ALTER TABLE device ADD COLUMN reviewed INTEGER NOT NULL DEFAULT 0;
UPDATE device SET reviewed = 1 WHERE source != 'scan';
