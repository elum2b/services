package sql

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	input := `
CREATE TABLE example (value TEXT DEFAULT ';');
-- a ; in a line comment
CREATE FUNCTION notify() RETURNS void AS $body$
BEGIN
    PERFORM ';';
END;
$body$ LANGUAGE plpgsql;
/* a ; block /* nested ; */ comment */
INSERT INTO example (value) VALUES ('it''s; fine');
`
	got, err := SplitStatements(input)
	if err != nil {
		t.Fatalf("SplitStatements() error: %v", err)
	}
	want := []string{
		"CREATE TABLE example (value TEXT DEFAULT ';')",
		"-- a ; in a line comment\nCREATE FUNCTION notify() RETURNS void AS $body$\nBEGIN\n    PERFORM ';';\nEND;\n$body$ LANGUAGE plpgsql",
		"/* a ; block /* nested ; */ comment */\nINSERT INTO example (value) VALUES ('it''s; fine')",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitStatements() = %#v, want %#v", got, want)
	}
}

func TestSplitStatementsRejectsUnterminatedSyntax(t *testing.T) {
	tests := []string{
		"SELECT 'unfinished",
		"SELECT \"unfinished",
		"/* unfinished",
		"SELECT $body$ unfinished",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := SplitStatements(input); err == nil {
				t.Fatal("SplitStatements() must reject unterminated SQL syntax")
			}
		})
	}
}
