-- +goose Up

CREATE VIRTUAL TABLE items_fts USING fts5(
    title,
    original_title,
    overview,
    content=items,
    content_rowid=rowid,
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER items_fts_insert AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, original_title)
    VALUES (NEW.rowid, NEW.title, NEW.original_title);
END;

CREATE TRIGGER items_fts_delete AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title)
    VALUES ('delete', OLD.rowid, OLD.title, OLD.original_title);
END;

CREATE TRIGGER items_fts_update AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title)
    VALUES ('delete', OLD.rowid, OLD.title, OLD.original_title);
    INSERT INTO items_fts(rowid, title, original_title)
    VALUES (NEW.rowid, NEW.title, NEW.original_title);
END;

-- +goose Down
DROP TRIGGER IF EXISTS items_fts_update;
DROP TRIGGER IF EXISTS items_fts_delete;
DROP TRIGGER IF EXISTS items_fts_insert;
DROP TABLE IF EXISTS items_fts;
