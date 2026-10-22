package migration

import (
	"fmt"
	"strings"
	"unicode"
)

// CheckSQL menolak SQL kosong dan statement yang nggak bisa jalan di dalam
// transaction. Migration selalu dijalankan dalam satu transaction, jadi
// statement seperti CREATE INDEX CONCURRENTLY atau VACUUM pasti gagal, dan
// BEGIN/COMMIT di dalam SQL bakal merusak jaminan rollback.
func CheckSQL(sql string) error {
	statements := splitStatements(blankLiterals(sql))
	if len(statements) == 0 {
		return ErrEmptySQL
	}
	for i, words := range statements {
		if reason := notTransactional(words); reason != "" {
			return fmt.Errorf("%w: statement ke-%d (%s)", ErrNotTransactional, i+1, reason)
		}
	}
	return nil
}

func notTransactional(words []string) string {
	first := words[0]
	switch first {
	case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "ABORT":
		return "kontrol transaction diatur platform"
	case "VACUUM":
		return "VACUUM"
	}
	if first == "PREPARE" && hasWord(words, "TRANSACTION") {
		return "PREPARE TRANSACTION"
	}
	if first == "ALTER" && len(words) > 1 && words[1] == "SYSTEM" {
		return "ALTER SYSTEM"
	}
	if (first == "CREATE" || first == "DROP") && len(words) > 1 && (words[1] == "DATABASE" || words[1] == "TABLESPACE") {
		return first + " " + words[1]
	}
	// REFRESH MATERIALIZED VIEW CONCURRENTLY boleh di dalam transaction, sisanya nggak.
	if first != "REFRESH" && hasWord(words, "CONCURRENTLY") {
		return "CONCURRENTLY"
	}
	return ""
}

func hasWord(words []string, w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}

// splitStatements memecah SQL per ';' lalu mengambil kata-katanya dalam huruf
// besar. Statement kosong dibuang.
func splitStatements(sql string) [][]string {
	var out [][]string
	for _, stmt := range strings.Split(sql, ";") {
		words := strings.FieldsFunc(strings.ToUpper(stmt), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
		})
		if len(words) > 0 {
			out = append(out, words)
		}
	}
	return out
}

// blankLiterals mengganti komentar, string, identifier ber-kutip, dan blok
// dollar-quote dengan spasi, supaya isinya (termasuk ';') nggak ikut dibaca.
func blankLiterals(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			i = skipUntil(sql, i+2, "\n")
		case strings.HasPrefix(sql[i:], "/*"):
			i = skipUntil(sql, i+2, "*/")
		case sql[i] == '\'' || sql[i] == '"':
			i = skipQuoted(sql, i)
		case sql[i] == '$':
			if tag, ok := dollarTag(sql[i:]); ok {
				i = skipUntil(sql, i+len(tag), tag)
			} else {
				b.WriteByte(sql[i])
				i++
				continue
			}
		default:
			b.WriteByte(sql[i])
			i++
			continue
		}
		b.WriteByte(' ')
	}
	return b.String()
}

func skipUntil(s string, from int, end string) int {
	if j := strings.Index(s[from:], end); j >= 0 {
		return from + j + len(end)
	}
	return len(s)
}

// skipQuoted melompati '...' atau "...", dengan kutip ganda sebagai escape.
func skipQuoted(s string, i int) int {
	q := s[i]
	for i++; i < len(s); i++ {
		if s[i] != q {
			continue
		}
		if i+1 < len(s) && s[i+1] == q {
			i++
			continue
		}
		return i + 1
	}
	return len(s)
}

// dollarTag mengenali pembuka dollar-quote seperti $$ atau $body$.
func dollarTag(s string) (string, bool) {
	for j := 1; j < len(s); j++ {
		c := rune(s[j])
		if c == '$' {
			return s[:j+1], true
		}
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
			return "", false
		}
	}
	return "", false
}
