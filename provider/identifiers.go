package provider

import "strings"

func quoteIdent(name string) string {
	return pgIdentifier{name}.Sanitize()
}

type pgIdentifier struct {
	name string
}

func (i pgIdentifier) Sanitize() string {
	return `"` + strings.ReplaceAll(i.name, `"`, `""`) + `"`
}

func quoteLiteral(s string) string {
	s = strings.ReplaceAll(s, `'`, `''`)
	if strings.Contains(s, `\`) {
		s = strings.ReplaceAll(s, `\`, `\\`)
		return `E'` + s + `'`
	}
	return `'` + s + `'`
}

var validObjectTypes = []string{"database", "schema", "table", "sequence"}

var validPrivileges = []string{
	"CONNECT", "CREATE", "TEMPORARY", "TEMP", // database
	"USAGE",                                                                     // schema, sequence
	"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER", // table
}
