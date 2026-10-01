package provider

import "testing"

func TestSessionRoleSetting(t *testing.T) {
	cases := []struct {
		name      string
		in        []string
		want      string
		wantFound bool
	}{
		{"nil", nil, "", false},
		{"no role entry", []string{"search_path=app", "statement_timeout=5s"}, "", false},
		{"only role", []string{"role=app_owner"}, "app_owner", true},
		{"among others", []string{"search_path=app", "role=app_owner"}, "app_owner", true},
		{"value with equals", []string{"role=a=b"}, "a=b", true},
		{"similar name", []string{"rolex=foo"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, found := sessionRoleSetting(c.in)
			if got != c.want || found != c.wantFound {
				t.Errorf("sessionRoleSetting(%q) = (%q, %v), want (%q, %v)", c.in, got, found, c.want, c.wantFound)
			}
		})
	}
}

func TestInDatabaseClause(t *testing.T) {
	if got := inDatabaseClause(""); got != "" {
		t.Errorf(`inDatabaseClause("") = %q, want ""`, got)
	}
	if got, want := inDatabaseClause(`app"db`), ` IN DATABASE "app""db"`; got != want {
		t.Errorf("inDatabaseClause = %q, want %q", got, want)
	}
}
