-- +goose Up

CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title,
    original_title,
    content='items',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, original_title)
    VALUES (new.rowid, new.title, new.original_title);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title)
    VALUES ('delete', old.rowid, old.title, old.original_title);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title)
    VALUES ('delete', old.rowid, old.title, old.original_title);
    INSERT INTO items_fts(rowid, title, original_title)
    VALUES (new.rowid, new.title, new.original_title);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS items_au;
DROP TRIGGER IF EXISTS items_ad;
DROP TRIGGER IF EXISTS items_ai;
DROP TABLE IF EXISTS items_fts;
